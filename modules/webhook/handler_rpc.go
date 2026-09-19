package webhook

import (
	"context"
	"errors"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/go-ozzo/ozzo-validation/v4"
	commonv1 "github.com/riipandi/tango/codegen/proto/go/tango/common/v1"
	webhookv1 "github.com/riipandi/tango/codegen/proto/go/tango/webhook/v1"
	webhookv1connect "github.com/riipandi/tango/codegen/proto/go/tango/webhook/v1/webhookv1connect"
	"github.com/riipandi/tango/internal/rpcerr"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/structpb"
)

// hookRPC adapts the webhook service to the generated Connect
// contract. The whole surface is admin-only, so the composition root
// wraps the mount with the admin guard.
type hookRPC struct {
	service *Service
}

// defaultTestEvent labels the synthetic delivery the Test procedure
// emits; subscribers of "*" or this name receive it.
const defaultTestEvent = "webhook.test"

// RPCService returns the Connect registration for the webhook
// surface.
func (m *Module) RPCService() (string, http.Handler) {
	prefix, handler := webhookv1connect.NewWebhookServiceHandler(&hookRPC{service: m.service}, rpcerr.RecoverOption())
	return prefix, handler
}

func (h *hookRPC) List(ctx context.Context, req *connect.Request[commonv1.PageRequest]) (*connect.Response[webhookv1.ListWebhooksResponse], error) {
	page := Page{Page: int(req.Msg.GetPage()), Limit: int(req.Msg.GetLimit())}
	hooks, total, err := h.service.List(ctx, ListParams{Page: page})
	if err != nil {
		return nil, rpcError(err)
	}
	out := make([]*webhookv1.Webhook, 0, len(hooks))
	for _, hook := range hooks {
		out = append(out, hookProto(hook))
	}
	return connect.NewResponse(&webhookv1.ListWebhooksResponse{
		Webhooks: out,
		Metadata: metadataFrom(page, total),
	}), nil
}

func (h *hookRPC) Create(ctx context.Context, req *connect.Request[webhookv1.CreateWebhookRequest]) (*connect.Response[webhookv1.Webhook], error) {
	params := CreateParams{
		Name:        req.Msg.GetName(),
		Description: req.Msg.Description,
		Endpoint:    req.Msg.GetEndpoint(),
		Method:      req.Msg.GetMethod(),
		Headers:     req.Msg.GetHeaders(),
		EventTypes:  req.Msg.GetEventTypes(),
	}
	if req.Msg.Enabled != nil {
		params.Enabled = req.Msg.Enabled
	}
	hook, err := h.service.Create(ctx, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(hookProto(hook)), nil
}

func (h *hookRPC) Get(ctx context.Context, req *connect.Request[webhookv1.GetWebhookRequest]) (*connect.Response[webhookv1.Webhook], error) {
	id, err := parseHookID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("webhook not found")
	}
	hook, err := h.service.Get(ctx, id)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(hookProto(hook)), nil
}

func (h *hookRPC) Update(ctx context.Context, req *connect.Request[webhookv1.UpdateWebhookRequest]) (*connect.Response[webhookv1.Webhook], error) {
	id, err := parseHookID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("webhook not found")
	}
	params := UpdateParams{
		Name:        req.Msg.Name,
		Description: req.Msg.Description,
		Endpoint:    req.Msg.Endpoint,
		Method:      req.Msg.Method,
		Headers:     req.Msg.GetHeaders(),
		Enabled:     req.Msg.Enabled,
		EventTypes:  req.Msg.GetEventTypes(),
	}
	hook, err := h.service.Update(ctx, id, params)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(hookProto(hook)), nil
}

func (h *hookRPC) Delete(ctx context.Context, req *connect.Request[webhookv1.DeleteWebhookRequest]) (*connect.Response[emptypb.Empty], error) {
	id, err := parseHookID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("webhook not found")
	}
	if err := h.service.Delete(ctx, id); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&emptypb.Empty{}), nil
}

func (h *hookRPC) RotateSecret(ctx context.Context, req *connect.Request[webhookv1.GetWebhookRequest]) (*connect.Response[webhookv1.RotateSecretResponse], error) {
	id, err := parseHookID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("webhook not found")
	}
	secret, err := h.service.RotateSecret(ctx, id)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&webhookv1.RotateSecretResponse{Secret: secret}), nil
}

func (h *hookRPC) Test(ctx context.Context, req *connect.Request[webhookv1.TestWebhookRequest]) (*connect.Response[webhookv1.TestWebhookResponse], error) {
	id, err := parseHookID(req.Msg.GetId())
	if err != nil {
		return nil, rpcerr.NotFound("webhook not found")
	}
	// The endpoint is addressed directly rather than through the
	// subscription filter: a test must reach a receiver even when its
	// subscription list does not name the synthetic event.
	logID, err := h.service.DeliverTo(ctx, id, defaultTestEvent, map[string]any{
		"event":  defaultTestEvent,
		"source": "tango",
	})
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&webhookv1.TestWebhookResponse{DeliveryId: logID.String()}), nil
}

func (h *hookRPC) ListDeliveries(ctx context.Context, req *connect.Request[webhookv1.ListDeliveriesRequest]) (*connect.Response[webhookv1.ListDeliveriesResponse], error) {
	id, err := parseHookID(req.Msg.GetWebhookId())
	if err != nil {
		return nil, rpcerr.NotFound("webhook not found")
	}
	return h.listDeliveries(ctx, &id, req.Msg.GetPage())
}

func (h *hookRPC) ListAllDeliveries(ctx context.Context, req *connect.Request[commonv1.PageRequest]) (*connect.Response[webhookv1.ListDeliveriesResponse], error) {
	return h.listDeliveries(ctx, nil, req.Msg)
}

func (h *hookRPC) listDeliveries(ctx context.Context, id *WebhookID, page *commonv1.PageRequest) (*connect.Response[webhookv1.ListDeliveriesResponse], error) {
	window := Page{Page: int(page.GetPage()), Limit: int(page.GetLimit())}
	deliveries, total, err := h.service.ListDeliveries(ctx, id, ListParams{Page: window})
	if err != nil {
		return nil, rpcError(err)
	}
	out := make([]*webhookv1.Delivery, 0, len(deliveries))
	for _, d := range deliveries {
		out = append(out, deliveryProto(d))
	}
	var metadata *commonv1.PageMetadata
	if window.Page >= 1 && window.Limit >= 1 {
		metadata = rpcerr.PageMetadata(window.Page, window.Limit, total)
	}
	return connect.NewResponse(&webhookv1.ListDeliveriesResponse{
		Deliveries: out,
		Metadata:   metadata,
	}), nil
}

// rpcError maps webhook domain sentinels onto Connect codes with the
// same wording the REST handler emits.
func rpcError(err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return rpcerr.NotFound("webhook not found")
	case errors.Is(err, ErrDuplicateName), errors.Is(err, ErrDisabled):
		return rpcerr.AlreadyExists(err.Error())
	case errors.Is(err, ErrTooLarge):
		return rpcerr.InvalidArgument(err.Error())
	default:
		// ozzo validation failures carry field detail — invalid
		// argument, not internal.
		var validationErr validation.Errors
		if errors.As(err, &validationErr) {
			return rpcerr.InvalidArgument("validation failed: " + err.Error())
		}
		return rpcerr.Internal("internal error")
	}
}

// parseHookID accepts only the TypeID form (webhook_...); raw UUIDs
// 404.
func parseHookID(raw string) (WebhookID, error) {
	return parseWebhookID(raw)
}

// metadataFrom builds the shared pagination block. An unpaged or
// all-marker page returns nil, so only paged responses carry metadata.
func metadataFrom(page Page, total int) *commonv1.PageMetadata {
	if page.Page < 1 || page.Limit < 1 {
		return nil
	}
	return rpcerr.PageMetadata(page.Page, page.Limit, total)
}

func hookProto(w Webhook) *webhookv1.Webhook {
	out := &webhookv1.Webhook{
		Id:         w.ID.String(),
		Name:       w.Name,
		Endpoint:   w.Endpoint,
		Method:     w.Method,
		Headers:    w.Headers,
		Enabled:    w.Enabled,
		EventTypes: w.EventTypes,
		CreatedAt:  w.CreatedAt.UTC().Format(time.RFC3339),
	}
	out.Headers = w.Headers
	if w.Description != nil {
		out.Description = w.Description
	}
	if w.Secret != nil && *w.Secret != "" {
		out.Secret = w.Secret
	}
	if w.UpdatedAt != nil {
		v := w.UpdatedAt.UTC().Format(time.RFC3339)
		out.UpdatedAt = &v
	}
	return out
}

func deliveryProto(d Delivery) *webhookv1.Delivery {
	out := &webhookv1.Delivery{
		Id:        d.ID.String(),
		Attempts:  rpcerr.ToInt32(d.Attempts),
		Succeeded: d.Succeeded,
		CreatedAt: d.CreatedAt.UTC().Format(time.RFC3339),
	}
	if d.WebhookID != nil {
		v := d.WebhookID.String()
		out.WebhookId = &v
	}
	if d.Event != nil {
		out.Event = d.Event
	}
	if d.HTTPStatus != nil {
		v := rpcerr.ToInt32(*d.HTTPStatus)
		out.HttpStatus = &v
	}
	if len(d.Response) > 0 {
		if resp, err := structpb.NewStruct(d.Response); err == nil {
			out.Response = resp
		}
	}
	if d.Error != nil {
		out.Error = d.Error
	}
	if d.DeliveredAt != nil {
		v := d.DeliveredAt.UTC().Format(time.RFC3339)
		out.DeliveredAt = &v
	}
	return out
}
