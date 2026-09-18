package transport

import (
	"context"
	"fmt"
	"net/http"

	"connectrpc.com/connect"
	jsonv2 "encoding/json/v2"
	systemv1 "github.com/riipandi/tango/gen/proto/go/tango/system/v1"
	"github.com/riipandi/tango/gen/proto/go/tango/system/v1/systemv1connect"
)

// rpcRecover maps panics inside RPC handlers onto Connect internal
// errors instead of letting the connection die without a response.
func rpcRecover() connect.HandlerOption {
	return connect.WithRecover(func(_ context.Context, _ connect.Spec, _ http.Header, r any) error {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("internal error: %v", r))
	})
}

// rpcHandler builds the Connect Protocol handler tree served below
// /rpc/. Public services mount directly; protected services are
// wrapped by BearerAuth in the phase that introduces them.
func rpcHandler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle(systemv1connect.NewHealthServiceHandler(
		&healthSmokeService{},
		rpcRecover(),
	))
	// Unknown procedures answer Connect errors, never the plain-text
	// ServeMux default or the SPA fallback.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_ = jsonv2.MarshalWrite(w, map[string]string{
			"code":    "not_found",
			"message": "procedure not found: " + r.URL.Path,
		})
	})
	return mux
}

// healthSmokeService backs the transport smoke RPC. It is not the
// readiness document; /healthz and /api/healthz remain the public
// health contracts.
type healthSmokeService struct{}

func (healthSmokeService) Check(_ context.Context, _ *connect.Request[systemv1.CheckRequest]) (*connect.Response[systemv1.CheckResponse], error) {
	return connect.NewResponse(&systemv1.CheckResponse{Status: "ok"}), nil
}
