package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	systemv1connect "github.com/riipandi/tango/codegen/proto/go/tango/system/v1/systemv1connect"
	"github.com/riipandi/tango/internal/health"
	"github.com/riipandi/tango/internal/kernel"
)

// RPCPath is the route prefix the ConnectRPC surface is mounted on. The SPA,
// the admin console, and internal tools call the generated clients below it.
const RPCPath = "/rpc"

// healthCheckPath is the full path the readiness procedure answers below the
// RPC prefix — the same readiness the REST probe answers at /api/healthz.
const healthCheckPath = RPCPath + systemv1connect.HealthServiceCheckProcedure

// The names the Connect protocol resolves a codec from, taken from the
// `Content-Type` a client sends. Both are registered, because a client may
// spell the JSON content type either way.
const (
	rpcCodecJSON            = "json"
	rpcCodecJSONCharsetUTF8 = "json; charset=utf-8"
)

// rpcJSONCodec adapts protojson to the contract the two transports share.
//
// protobuf's JSON mapping defaults to lowerCamelCase, which would make an RPC
// field (`statusCode`) and its REST twin (`status_code`) two spellings of one
// contract. `UseProtoNames` serializes under the declared proto field names
// instead, so both transports write snake_case and a client reads one
// vocabulary. Requests stay tolerant: protojson accepts either spelling on the
// way in, and unknown fields are discarded so a client is not pinned to the
// server's schema version.
//
// It is registered under the names connect's built-in JSON codecs use, which
// replaces them rather than adding a second JSON encoding beside them.
type rpcJSONCodec struct {
	name string
}

var _ connect.Codec = rpcJSONCodec{}

func (c rpcJSONCodec) Name() string { return c.name }

func (c rpcJSONCodec) Marshal(message any) ([]byte, error) {
	msg, err := protoMessage(message)
	if err != nil {
		return nil, err
	}
	return protojson.MarshalOptions{UseProtoNames: true}.Marshal(msg)
}

func (c rpcJSONCodec) MarshalAppend(dst []byte, message any) ([]byte, error) {
	msg, err := protoMessage(message)
	if err != nil {
		return nil, err
	}
	return protojson.MarshalOptions{UseProtoNames: true}.MarshalAppend(dst, msg)
}

func (c rpcJSONCodec) Unmarshal(data []byte, message any) error {
	msg, err := protoMessage(message)
	if err != nil {
		return err
	}
	return protojson.UnmarshalOptions{DiscardUnknown: true}.Unmarshal(data, msg)
}

// protoMessage checks the codec was handed a message it can serialize. A
// generated handler always passes one, so this reports a hand-registered
// service rather than a runtime condition.
func protoMessage(message any) (proto.Message, error) {
	msg, ok := message.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("rpc: %T is not a protobuf message", message)
	}
	return msg, nil
}

// rpcHandlerOptions are the options every service on this surface is
// registered with: the shared codec pair and the panic boundary. A module
// that serves procedures receives them and passes them to every generated
// handler it registers, so a module's procedure answers exactly like the
// transport's own. The correlation id is not here on purpose: the request
// middleware writes it on the writer before any handler runs, and connect
// merges handler-set headers by appending, so a second writer would answer
// with the header twice.
func rpcHandlerOptions() []connect.HandlerOption {
	return []connect.HandlerOption{
		connect.WithCodec(rpcJSONCodec{name: rpcCodecJSON}),
		connect.WithCodec(rpcJSONCodec{name: rpcCodecJSONCharsetUTF8}),
		connect.WithRecover(func(_ context.Context, _ connect.Spec, _ http.Header, recovered any) error {
			// The recovery middleware above this surface answers a panic with
			// the REST envelope, which a Connect client cannot parse. A panic
			// inside a procedure is reported in the protocol the caller used,
			// and the panic value is not published: it is an internal detail,
			// and the middleware logs it with the stack and the request id.
			return connect.NewError(connect.CodeInternal, errors.New("internal error"))
		}),
	}
}

// mountRPC registers the ConnectRPC surface on the router. It receives
// exactly what the surface serves — the checker and the modules — so the RPC
// registration never reads how the router got its dependencies.
func mountRPC(r chi.Router, checker *health.Checker, modules []kernel.Module) {
	// The prefix is stripped because chi only shifts its own route context: a
	// generated Connect handler matches its procedure path exactly.
	r.Mount(RPCPath, http.StripPrefix(RPCPath, rpcRouter(checker, modules)))
}

// rpcRouter builds the Connect handler tree served below RPCPath.
//
// Every procedure is registered at its own path rather than the service's
// shared subtree prefix: the generated handler answers a path under its prefix
// it does not know with a plain-text 404, which a Connect client cannot read.
// Registering the procedures keeps the not-found boundary in one place, where
// the answer is written in the protocol the caller used.
//
// The path is registered for every method on purpose. A procedure is POST-only
// (no procedure in this contract declares `idempotency_level =
// NO_SIDE_EFFECTS`, which is what would make a GET legal), and the generated
// handler is what refuses another method with `405` and `Allow: POST`.
func rpcRouter(checker *health.Checker, modules []kernel.Module) http.Handler {
	r := chi.NewRouter()

	options := rpcHandlerOptions()
	_, healthHandler := systemv1connect.NewHealthServiceHandler(
		newRPCHealthService(checker),
		options...,
	)
	r.Handle(systemv1connect.HealthServiceCheckProcedure, healthHandler)

	// A module's procedures mount beside the transport's own, on the same
	// router, so they share the codec and the not-found boundary.
	kernel.MountRPC(r, options, modules...)

	// The error writer owns the wire form of a miss, so the answer carries the
	// code and the HTTP status the Connect specification assigns it rather
	// than a shape invented here or the SPA document.
	writer := connect.NewErrorWriter(options...)
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeRPCError(writer, w, r, connect.CodeUnimplemented,
			fmt.Sprintf("unknown procedure %q", r.URL.Path))
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeRPCError(writer, w, r, connect.CodeUnimplemented,
			fmt.Sprintf("procedure %q does not accept %s", r.URL.Path, r.Method))
	})

	return r
}

// writeRPCError answers a request that reached no procedure, in the protocol the caller used.
func writeRPCError(writer *connect.ErrorWriter, w http.ResponseWriter, r *http.Request, code connect.Code, message string) {
	_ = writer.Write(w, r, connect.NewError(code, errors.New(message)))
}

// rpcRefuse answers a limited procedure call in the protocol the caller used:
// `resource_exhausted`, the code the Connect specification maps to 429. The
// X-RateLimit-* and Retry-After headers are already on the response — the
// middleware wrote them before refusing. The error writer is built from the
// same handler options the procedures are registered with, so the refusal is
// serialized under the shared codec, exactly like a refusal from a procedure
// itself.
func rpcRefuse(w http.ResponseWriter, r *http.Request) {
	options := rpcHandlerOptions()
	writer := connect.NewErrorWriter(options...)
	writeRPCError(writer, w, r, connect.CodeResourceExhausted, "rate limit exceeded")
}
