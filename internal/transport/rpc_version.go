package transport

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	systemv1 "github.com/riipandi/tango/codegen/proto/go/tango/system/v1"
	systemv1connect "github.com/riipandi/tango/codegen/proto/go/tango/system/v1/systemv1connect"
	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/transport/middleware"
)

// versionCurrentProcedure is the only protected method of the version
// surface; Latest answers anonymously.
const versionCurrentProcedure = "/tango.system.v1.VersionService/Current"

// VersionRPCService exposes the deployed build metadata over /rpc.
// Current authenticates from `Authorization: Bearer` only — via the
// per-procedure interceptor, because the service mixes public and
// protected methods. The handler-level principal check stays as a
// defense in depth for callers that bypass the interceptor.
func VersionRPCService(latest LatestVersionSource, auth kernel.AccessAuthenticator) (string, http.Handler) {
	opts := rpcerr.Options()
	if auth != nil {
		opts = append(opts, connect.WithInterceptors(bearerInterceptor{auth: auth}))
	}
	return systemv1connect.NewVersionServiceHandler(&versionRPCService{latest: latest}, opts...)
}

// bearerInterceptor resolves the internal access token for protected
// procedures before the handler runs. Streaming hooks are absent by
// design: the first-party surface is unary only.
type bearerInterceptor struct {
	auth kernel.AccessAuthenticator
}

func (i bearerInterceptor) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (i bearerInterceptor) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return next
}

func (i bearerInterceptor) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		if req.Spec().Procedure == versionCurrentProcedure {
			principal, err := middleware.ResolveBearer(ctx, i.auth, req.Header())
			if err != nil {
				return nil, err
			}
			ctx = middleware.WithPrincipal(ctx, principal)
		}
		return next(ctx, req)
	}
}

type versionRPCService struct {
	latest LatestVersionSource
}

func (s *versionRPCService) Current(ctx context.Context, _ *connect.Request[systemv1.CurrentRequest]) (*connect.Response[systemv1.CurrentResponse], error) {
	if _, ok := middleware.PrincipalFromContext(ctx); !ok {
		return nil, rpcerr.Unauthenticated("bearer token required")
	}
	return connect.NewResponse(&systemv1.CurrentResponse{CurrentVersion: config.AppVersion}), nil
}

func (s *versionRPCService) Latest(_ context.Context, _ *connect.Request[systemv1.LatestRequest]) (*connect.Response[systemv1.LatestResponse], error) {
	version := config.AppVersion
	if s.latest != nil {
		version = s.latest.Latest()
	}
	return connect.NewResponse(&systemv1.LatestResponse{LatestVersion: version}), nil
}
