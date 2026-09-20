package transport

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	jsonv2 "encoding/json/v2"
	"github.com/go-chi/chi/v5"
	systemv1 "github.com/riipandi/tango/codegen/proto/go/tango/system/v1"
	"github.com/riipandi/tango/codegen/proto/go/tango/system/v1/systemv1connect"
	"github.com/riipandi/tango/internal/rpcerr"
)

// newRPCRouter builds the Connect handler tree served below /rpc/.
// Module-owned services register exact procedure subtrees via the
// mount callback; unknown procedures answer Connect errors, never
// the plain-text default or the SPA fallback. Callers must strip the
// /rpc prefix before mounting: the generated handlers match
// r.URL.Path exactly.
func newRPCRouter(mount func(chi.Router)) chi.Router {
	r := chi.NewRouter()
	r.NotFound(connectNotFound)
	r.MethodNotAllowed(connectMethodNotAllowed)

	prefix, smoke := systemv1connect.NewHealthServiceHandler(
		&healthSmokeService{},
		rpcerr.Options()...,
	)
	r.Handle(prefix+"*", smoke)

	if mount != nil {
		mount(r)
	}
	return r
}

// connectNotFound answers unknown procedures with the Connect error
// document.
func connectNotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = jsonv2.MarshalWrite(w, map[string]string{
		"code":    "not_found",
		"message": "procedure not found: " + r.URL.Path,
	})
}

// connectMethodNotAllowed answers non-POST calls to unary procedures
// with the Connect error document.
func connectMethodNotAllowed(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusMethodNotAllowed)
	_ = jsonv2.MarshalWrite(w, map[string]string{
		"code":    "method_not_allowed",
		"message": "unary procedures accept POST only",
	})
}

// healthSmokeService backs the transport smoke RPC. It is not the
// readiness document; /healthz and /api/healthz remain the public
// health contracts.
type healthSmokeService struct{}

func (healthSmokeService) Check(_ context.Context, _ *connect.Request[systemv1.CheckRequest]) (*connect.Response[systemv1.CheckResponse], error) {
	return connect.NewResponse(&systemv1.CheckResponse{Status: "ok"}), nil
}
