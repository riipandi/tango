package appconfig

import (
	"context"
	"net/http"

	"connectrpc.com/connect"
	adminv1 "github.com/riipandi/tango/codegen/proto/go/tango/admin/v1"
	adminv1connect "github.com/riipandi/tango/codegen/proto/go/tango/admin/v1/adminv1connect"
	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/internal/rpcerr"
	"github.com/riipandi/tango/internal/transport/middleware"
	"google.golang.org/protobuf/types/known/emptypb"
)

// configRPC adapts the settings module to the generated Connect
// contract. Reads and updates are admin-only (the composition root
// wraps the mount); the sensitive-value redaction stays in the views.
type configRPC struct {
	module *Module
}

// RPCService returns the Connect registration for the application
// configuration surface. The public bootstrap view is anonymous; the
// admin read, update, and test email guard per procedure.
func (m *Module) RPCService(access kernel.AccessAuthenticator) (string, http.Handler) {
	admin := map[string]bool{
		adminv1connect.ApplicationConfigurationServiceGetAllProcedure:    true,
		adminv1connect.ApplicationConfigurationServiceUpdateProcedure:    true,
		adminv1connect.ApplicationConfigurationServiceTestEmailProcedure: true,
	}
	prefix, handler := adminv1connect.NewApplicationConfigurationServiceHandler(&configRPC{module: m},
		connect.WithInterceptors(middleware.RPCPrincipalGuard(access, admin, nil)),
		rpcerr.RecoverOption(),
	)
	return prefix, handler
}

func (h *configRPC) Get(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[adminv1.ListConfigVariablesResponse], error) {
	variables, err := h.module.publicVariables(ctx)
	if err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	return connect.NewResponse(&adminv1.ListConfigVariablesResponse{Variables: variables}), nil
}

func (h *configRPC) GetAll(ctx context.Context, _ *connect.Request[emptypb.Empty]) (*connect.Response[adminv1.ListConfigVariablesResponse], error) {
	variables, err := h.module.allVariables(ctx)
	if err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	return connect.NewResponse(&adminv1.ListConfigVariablesResponse{Variables: variables}), nil
}

func (h *configRPC) Update(ctx context.Context, req *connect.Request[adminv1.UpdateConfigVariablesRequest]) (*connect.Response[adminv1.ListConfigVariablesResponse], error) {
	values := map[string]string{}
	var clear []string
	for _, v := range req.Msg.GetVariables() {
		key := v.GetKey()
		entry, known := lookup(key)
		if !known {
			continue // unknown keys are ignored, not rejected
		}
		if err := validateValue(entry, v.GetValue()); err != nil {
			return nil, rpcerr.InvalidArgument("validation failed: " + key + ": " + err.Error())
		}
		if entry.Sensitive && v.GetValue() == "" {
			// An empty sensitive value clears the stored secret;
			// the enc: check rejects empty rows, so clearing means
			// deleting.
			clear = append(clear, key)
			continue
		}
		values[key] = v.GetValue()
	}
	if err := h.module.writeValues(ctx, values, clear); err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	variables, err := h.module.allVariables(ctx)
	if err != nil {
		return nil, rpcerr.Internal("internal error")
	}
	return connect.NewResponse(&adminv1.ListConfigVariablesResponse{Variables: variables}), nil
}

func (h *configRPC) TestEmail(ctx context.Context, req *connect.Request[emptypb.Empty]) (*connect.Response[emptypb.Empty], error) {
	if h.module.mailer == nil {
		return nil, rpcerr.NotFound("test email is not available")
	}
	// Default to the signed-in administrator.
	to := req.Header().Get("X-Test-Email")
	if to == "" {
		principal, ok := middleware.PrincipalFromContext(ctx)
		if !ok || principal.Email == "" {
			return nil, rpcerr.InvalidArgument("recipient email required")
		}
		to = principal.Email
	}
	if err := h.module.mailer.EnqueueEmail(ctx, mailer.Message{
		To:       to,
		Subject:  "SMTP test email",
		Template: "test-email",
	}); err != nil {
		return nil, rpcerr.Internal("failed to queue email")
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

// publicVariables renders the unauthenticated bootstrap payload.
func (m *Module) publicVariables(ctx context.Context) ([]*adminv1.ConfigVariable, error) {
	overrides, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	merged, err := m.merged(ctx, overrides)
	if err != nil {
		return nil, err
	}
	out := make([]*adminv1.ConfigVariable, 0, len(configKeys))
	for _, entry := range configKeys {
		if !entry.Public {
			continue
		}
		out = append(out, configVariable(entry, merged[entry.Key]))
	}
	return out, nil
}

// allVariables renders every key for the admin view; sensitive
// values list as empty strings.
func (m *Module) allVariables(ctx context.Context) ([]*adminv1.ConfigVariable, error) {
	overrides, err := m.store.List(ctx)
	if err != nil {
		return nil, err
	}
	merged, err := m.merged(ctx, overrides)
	if err != nil {
		return nil, err
	}
	out := make([]*adminv1.ConfigVariable, 0, len(configKeys))
	for _, entry := range configKeys {
		value := merged[entry.Key]
		if entry.Sensitive {
			value = ""
		}
		out = append(out, configVariable(entry, value))
	}
	return out, nil
}

// writeValues persists the upserts and clears, sealing sensitive
// values first — the same discipline the REST update applies.
func (m *Module) writeValues(ctx context.Context, values map[string]string, clear []string) error {
	if err := m.sealSensitive(values); err != nil {
		return err
	}
	if err := m.store.Upsert(ctx, values); err != nil {
		return err
	}
	return m.store.Delete(ctx, clear)
}

func configVariable(entry configKey, value string) *adminv1.ConfigVariable {
	return &adminv1.ConfigVariable{
		Key:      entry.Key,
		Type:     configVariableType(entry.Type),
		Value:    value,
		IsPublic: protoBool(entry.Public),
	}
}

func configVariableType(t valueType) adminv1.ConfigVariable_Type {
	switch t {
	case typeInt:
		return adminv1.ConfigVariable_TYPE_INT
	case typeBool:
		return adminv1.ConfigVariable_TYPE_BOOL
	default:
		return adminv1.ConfigVariable_TYPE_STRING
	}
}

func protoBool(v bool) *bool { return &v }
