package auditlog

import (
	"context"
	"net/http"
	"time"
	"uuid"

	"connectrpc.com/connect"
	adminv1 "github.com/riipandi/tango/gen/proto/go/tango/admin/v1"
	adminv1connect "github.com/riipandi/tango/gen/proto/go/tango/admin/v1/adminv1connect"
	commonv1 "github.com/riipandi/tango/gen/proto/go/tango/common/v1"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/transport/middleware"
	"go.jetify.com/typeid"
	"google.golang.org/protobuf/types/known/structpb"
)

// logRPC adapts the audit module to the generated Connect contract.
// List is self-scoped; ListAll and FilterOptions are admin-only — the
// composition root wraps the mount with the bearer middleware and the
// handler guards the admin procedures per procedure.
type logRPC struct {
	module *Module
}

// RPCService returns the Connect registration for the audit log read
// surface. The self listing resolves the bearer; the admin listing
// and filter facets demand an admin, guarded per procedure.
func (m *Module) RPCService(access kernel.AccessAuthenticator) (string, http.Handler) {
	admin := map[string]bool{
		adminv1connect.AuditLogServiceListAllProcedure:       true,
		adminv1connect.AuditLogServiceFilterOptionsProcedure: true,
	}
	self := map[string]bool{
		adminv1connect.AuditLogServiceListProcedure: true,
	}
	prefix, handler := adminv1connect.NewAuditLogServiceHandler(&logRPC{module: m},
		connect.WithInterceptors(middleware.RPCPrincipalGuard(access, admin, self)),
		rpcerr.RecoverOption(),
	)
	return prefix, handler
}

func (h *logRPC) List(ctx context.Context, req *connect.Request[adminv1.ListAuditLogsRequest]) (*connect.Response[adminv1.ListAuditLogsResponse], error) {
	principal, ok := middleware.PrincipalFromContext(ctx)
	if !ok || principal.UserID == "" {
		return nil, rpcerr.Unauthenticated("bearer token required")
	}
	filters := ListFilters{UserID: h.module.uuidUserID(principal.UserID)}
	return h.list(ctx, req.Msg, filters)
}

func (h *logRPC) ListAll(ctx context.Context, req *connect.Request[adminv1.ListAuditLogsRequest]) (*connect.Response[adminv1.ListAuditLogsResponse], error) {
	return h.list(ctx, req.Msg, ListFilters{})
}

func (h *logRPC) list(ctx context.Context, msg *adminv1.ListAuditLogsRequest, scope ListFilters) (*connect.Response[adminv1.ListAuditLogsResponse], error) {
	filters := scope
	filters.Event = msg.GetEvent()
	if raw := msg.GetUserId(); scope.UserID == "" && raw != "" {
		if !validUUID(raw) {
			return nil, rpcerr.InvalidArgument("user_id must be a UUID or typed user ID")
		}
		filters.UserID = h.module.uuidUserID(raw)
	}
	if raw := msg.GetFrom(); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, rpcerr.InvalidArgument("from must be RFC3339")
		}
		filters.From = &parsed
	}
	if raw := msg.GetTo(); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			return nil, rpcerr.InvalidArgument("to must be RFC3339")
		}
		filters.To = &parsed
	}

	page := Page{Page: int(msg.GetPage().GetPage()), Limit: int(msg.GetPage().GetLimit())}
	entries, total, err := h.module.store.List(ctx, filters, page)
	if err != nil {
		return nil, rpcerr.Internal("internal error")
	}

	logs := make([]*adminv1.AuditLog, 0, len(entries))
	for _, e := range entries {
		logs = append(logs, entryProto(e))
	}
	var metadata *commonv1.PageMetadata
	if page.Page >= 1 && page.Limit >= 1 {
		metadata = rpcerr.PageMetadata(page.Page, page.Limit, total)
	}
	return connect.NewResponse(&adminv1.ListAuditLogsResponse{Logs: logs, Metadata: metadata}), nil
}

func (h *logRPC) FilterOptions(ctx context.Context, req *connect.Request[adminv1.FilterOptionsRequest]) (*connect.Response[adminv1.FilterOptionsResponse], error) {
	var (
		values []string
		err    error
	)
	switch req.Msg.GetKind() {
	case "client-names":
		values, err = h.module.store.ClientNameFilterValues(ctx)
	case "users":
		values, err = h.module.store.UserFilterValues(ctx)
	default:
		return nil, rpcerr.InvalidArgument("kind must be client-names or users")
	}
	if err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	return connect.NewResponse(&adminv1.FilterOptionsResponse{Values: values}), nil
}

// uuidUserID normalizes a typed ID string (user_...) or a bare UUID
// to the UUID column form, keeping the module decoupled from the
// user package.
func (m *Module) uuidUserID(raw string) string {
	if id, err := typeid.FromString(raw); err == nil && !id.IsZero() {
		return id.UUID()
	}
	return raw
}

// validUUID reports whether raw parses as a bare UUID or a typed ID;
// anything else is rejected before it reaches the store.
func validUUID(raw string) bool {
	if _, err := uuid.Parse(raw); err == nil {
		return true
	}
	id, err := typeid.FromString(raw)
	return err == nil && !id.IsZero()
}

func entryProto(e Entry) *adminv1.AuditLog {
	out := &adminv1.AuditLog{
		Id:        e.ID.String(),
		Event:     e.Event,
		Trigger:   string(e.Trigger),
		Status:    string(e.Status),
		CreatedAt: e.CreatedAt.UTC().Format(time.RFC3339),
	}
	if len(e.Payload) > 0 {
		payload, err := structpb.NewStruct(e.Payload)
		if err == nil {
			out.Payload = payload
		}
	}
	if e.ResourceType != "" {
		out.ResourceType = &e.ResourceType
	}
	out.ResourceId = optString(e.ResourceID)
	out.UserId = optString(e.UserID)
	out.IpAddress = optString(e.IPAddress)
	out.UserAgent = optString(e.UserAgent)
	if e.Device != "" {
		out.Device = &e.Device
	}
	return out
}

func optString(v *string) *string {
	if v == nil || *v == "" {
		return nil
	}
	return v
}
