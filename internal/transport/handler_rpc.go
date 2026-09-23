package transport

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"time"

	"connectrpc.com/connect"
	"github.com/go-chi/chi/v5"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	systemv1 "github.com/riipandi/tango/codegen/proto/go/tango/system/v1"
	"github.com/riipandi/tango/codegen/proto/go/tango/system/v1/systemv1connect"
	"github.com/riipandi/tango/internal/health"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/pkg/responder"
)

// RPCPath is the route prefix the ConnectRPC surface is mounted on. The SPA,
// the admin console, and internal tools call the generated clients below it.
const RPCPath = "/rpc"

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
// registered with: the shared codec pair, and the panic boundary.
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
//
// The caller mounts this with the prefix stripped: a generated Connect handler
// matches its procedure path exactly.
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

// writeRPCError answers a request that reached no procedure, in the protocol
// the caller used.
func writeRPCError(writer *connect.ErrorWriter, w http.ResponseWriter, r *http.Request, code connect.Code, message string) {
	_ = writer.Write(w, r, connect.NewError(code, errors.New(message)))
}

// rpcHealthService answers the readiness procedure over ConnectRPC.
//
// It publishes what the REST endpoint publishes, from the same checker, so the
// two surfaces cannot drift: one check set, one cache, one aggregate.
type rpcHealthService struct {
	checker *health.Checker
}

func newRPCHealthService(checker *health.Checker) *rpcHealthService {
	return &rpcHealthService{checker: checker}
}

// Check runs the checks and answers with the readiness document.
//
// An unhealthy system fails with `unavailable`, which the transport answers as
// 503 — the status a probe acts on — and the message names the checks that are
// down, because a bare status does not say what to look at.
func (s *rpcHealthService) Check(ctx context.Context, _ *connect.Request[systemv1.CheckRequest]) (*connect.Response[systemv1.CheckResponse], error) {
	// A router built without a checker is a wiring defect; the registry builds
	// the two together, so a working composition cannot reach this.
	if s.checker == nil {
		return nil, connect.NewError(connect.CodeUnavailable, errors.New("health check unavailable"))
	}

	result := s.checker.Check(ctx)
	if !result.Healthy() {
		err := connect.NewError(connect.CodeUnavailable, errors.New(health.Message(result)))
		// A failed call carries no metadata block, so the correlation id
		// travels in the header instead — the one place both transports can
		// always name the request.
		if id := responder.RequestIDFromContext(ctx); id != "" {
			err.Meta().Set(responder.RequestIDHeader, id)
		}
		return nil, err
	}

	return connect.NewResponse(&systemv1.CheckResponse{
		Metadata: rpcMetadata(ctx, http.StatusOK),
		Status:   string(result.Status),
		Details:  rpcCheckDetails(result),
		TookMs:   milliseconds(result.Duration),
	}), nil
}

// rpcMetadata builds the shared metadata block for one response. It writes
// what the REST envelope writes for the same request, so a client reads one
// status code and one correlation id from either transport.
func rpcMetadata(ctx context.Context, statusCode int32) *commonv1.ResponseMetadata {
	meta := &commonv1.ResponseMetadata{StatusCode: &statusCode}
	if id := responder.RequestIDFromContext(ctx); id != "" {
		meta.RequestId = &id
	}
	return meta
}

// rpcCheckDetails renders the per-check results in check-name order, which is
// the order the REST body writes them in.
func rpcCheckDetails(result health.Result) []*systemv1.CheckDetail {
	names := slices.Sorted(maps.Keys(result.Details))
	details := make([]*systemv1.CheckDetail, 0, len(names))
	for _, name := range names {
		detail := result.Details[name]
		details = append(details, &systemv1.CheckDetail{
			Name:     detail.Name,
			Status:   string(detail.Status),
			Target:   detail.Target,
			Error:    detail.Error,
			Optional: detail.Optional,
			TookMs:   milliseconds(detail.Duration),
		})
	}
	return details
}

// milliseconds renders a duration as fractional milliseconds, the unit both
// transports publish.
func milliseconds(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
