package oidc

// device.go implements the OAuth 2.0 Device Authorization Grant
// (RFC 8628) use cases: a limited-input device obtains a device
// code and user code, the user approves in a browser, and the device
// polls the token endpoint. Distinct from the identity devicelogin
// surface. The HTTP shells live in handler_device.go.

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/riipandi/tango/internal/transport/middleware"
)

// deviceAuthorizationResponse is the RFC 8628 §3.2 payload;
// verification_uri_complete serves QR-code flows.
type deviceAuthorizationResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// deviceCodeInfo is the browser-side payload for the consent page.
type deviceCodeInfo struct {
	ClientID   string `json:"client_id"`
	ClientName string `json:"client_name"`
	Scope      string `json:"scope"`
}

// userCodeAlphabet excludes look-alike characters (0/O/1/I).
const userCodeAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789"

// apiFailure is one responder-envelope failure rendered by the
// browser-facing endpoints: status plus the fixed public message.
// Plain data — the transport layer renders it.
type apiFailure struct {
	Status  int
	Message string
}

func apiFailureOf(status int, message string) *apiFailure {
	return &apiFailure{Status: status, Message: message}
}

// createDeviceAuthorization mints the device + user code pair and
// persists the pending authorization.
func (s *Service) createDeviceAuthorization(ctx context.Context, client Client, scope, resource, nonce string) (*deviceAuthorizationResponse, *tokenFailure) {
	if !scopeList(scope)[ScopeOpenID] {
		return nil, &tokenFailure{Code: "invalid_scope", Status: 400}
	}

	deviceCode, err := randomToken()
	if err != nil {
		return nil, serverError()
	}
	userCode, err := newUserCode()
	if err != nil {
		return nil, serverError()
	}

	code := DeviceCode{
		DeviceCodeHash: sha256Hex(deviceCode),
		UserCodeHash:   sha256Hex(normalizeUserCode(userCode)),
		Scope:          scope,
		Resource:       resource,
		Nonce:          nonce,
		ClientID:       client.ID.String(),
		Status:         DeviceStatusPending,
		ExpiresAt:      time.Now().UTC().Add(s.DeviceCodeTTL()),
	}
	if err := s.store.InsertDeviceCode(ctx, code); err != nil {
		return nil, serverError()
	}

	s.record(ctx, "oidc_device_authorization_created", map[string]any{
		"client_id": client.ID.String(),
	})
	return &deviceAuthorizationResponse{
		DeviceCode:              deviceCode,
		UserCode:                userCode,
		VerificationURI:         s.issuer + DeviceVerificationPath,
		VerificationURIComplete: s.issuer + DeviceVerificationPath + "?code=" + userCode,
		ExpiresIn:               int(s.DeviceCodeTTL().Seconds()),
		Interval:                DevicePollInterval,
	}, nil
}

// decideDevice approves or denies a pending authorization from the
// browser session. action "deny" denies; anything else approves.
// The returned clientID is empty when the flow failed early.
func (s *Service) decideDevice(ctx context.Context, principal middleware.Principal, userCode, action string) (string, *apiFailure) {
	hash := sha256Hex(normalizeUserCode(userCode))
	code, err := s.store.GetDeviceCodeByUserCode(ctx, hash)
	if err != nil {
		return "", apiFailureOf(400, "invalid user code")
	}
	if code.ExpiresAt.Before(time.Now().UTC()) {
		return "", apiFailureOf(400, "device code expired")
	}
	if code.Status != DeviceStatusPending {
		// A code may be approved only once: no rebinding to a second
		// user before the device polls.
		return "", apiFailureOf(409, "device code already decided")
	}

	client, err := s.store.GetClient(ctx, OIDCClientIDFromClientKey(code.ClientID))
	if err != nil {
		return "", apiFailureOf(400, "invalid user code")
	}
	if client.IsGroupRestricted && !s.userAllowed(ctx, client, principal.UserID) {
		return "", apiFailureOf(403, "user is not allowed to use this client")
	}

	if action == "deny" {
		if err := s.store.DenyDeviceCode(ctx, hash); err != nil {
			return "", apiFailureOf(500, "failed to deny device code")
		}
		s.record(ctx, "oidc_device_authorization_denied", map[string]any{
			"client_id": client.ID.String(),
			"user_id":   principal.UserID,
		})
		return client.ID.String(), nil
	}

	// The RFC 8707 resource resolves at approval time: the granted
	// audience and scope subset are what the poll will mint.
	audience, granted, errName := s.resolveResource(ctx, client.ID.String(), code.Resource, code.Scope, SubjectUser)
	if errName != "" {
		return "", apiFailureOf(403, "resource indicator is not allowed for this client")
	}
	if err := s.store.ApproveDeviceCode(ctx, hash, principal.UserID); err != nil {
		return "", apiFailureOf(500, "failed to approve device code")
	}
	if err := s.store.UpdateDeviceCodeGrant(ctx, hash, audience, strings.Join(granted, " ")); err != nil {
		return "", apiFailureOf(500, "failed to record the grant")
	}
	if err := s.store.UpsertAuthorizedClient(ctx, principal.UserID, client.ID.String(), granted); err != nil {
		return "", apiFailureOf(500, "failed to record consent")
	}
	s.record(ctx, "oidc_device_authorization_approved", map[string]any{
		"client_id": client.ID.String(),
		"user_id":   principal.UserID,
	})
	return client.ID.String(), nil
}

// deviceInfo resolves what the consent page shows for a user code.
func (s *Service) deviceInfo(ctx context.Context, userCode string) (*deviceCodeInfo, *apiFailure) {
	hash := sha256Hex(normalizeUserCode(userCode))
	code, err := s.store.GetDeviceCodeByUserCode(ctx, hash)
	if err != nil {
		return nil, apiFailureOf(400, "invalid user code")
	}
	if code.ExpiresAt.Before(time.Now().UTC()) {
		return nil, apiFailureOf(400, "device code expired")
	}

	client, err := s.store.GetClient(ctx, OIDCClientIDFromClientKey(code.ClientID))
	if err != nil {
		return nil, apiFailureOf(400, "invalid user code")
	}
	return &deviceCodeInfo{
		ClientID:   client.ID.String(),
		ClientName: client.Name,
		Scope:      code.Scope,
	}, nil
}

// exchangeDevice implements the urn:ietf:params:oauth:grant-type:
// device_code grant: poll until approved. Error codes follow RFC
// 8628 §3.5 — authorization_pending, slow_down, expired_token.
func (s *Service) exchangeDevice(ctx context.Context, client Client, deviceCode string) (*tokenResponse, *tokenFailure) {
	if deviceCode == "" {
		return nil, invalidRequest()
	}

	hash := sha256Hex(deviceCode)
	code, err := s.store.GetDeviceCode(ctx, hash)
	if err != nil || code.ClientID != client.ID.String() {
		return nil, invalidGrant()
	}

	now := time.Now().UTC()

	// Final states answer directly; slow-down discipline only
	// governs the pending wait.
	switch code.Status {
	case DeviceStatusDenied:
		return nil, &tokenFailure{Code: "access_denied", Status: 403}
	case DeviceStatusApproved:
		return s.consumeDeviceToken(ctx, client, hash, code)
	}

	// Pending: poll faster than the advertised interval and the wait
	// escalates (§3.5 slow_down); an expired authorization dies.
	if code.ExpiresAt.Before(now) {
		return nil, &tokenFailure{Code: "expired_token", Status: 400}
	}
	if code.LastPolledAt != nil && now.Sub(*code.LastPolledAt) < DevicePollInterval*time.Second {
		_ = s.store.TouchDevicePoll(ctx, hash, now)
		return nil, &tokenFailure{Code: "slow_down", Status: 400}
	}
	_ = s.store.TouchDevicePoll(ctx, hash, now)
	return nil, &tokenFailure{Code: "authorization_pending", Status: 400}
}

// consumeDeviceToken flips the approved authorization to consumed
// and mints the token response.
func (s *Service) consumeDeviceToken(ctx context.Context, client Client, hash string, code DeviceCode) (*tokenResponse, *tokenFailure) {
	consumed, err := s.store.ConsumeDeviceCode(ctx, hash)
	if err != nil {
		return nil, &tokenFailure{Code: "expired_token", Status: 400}
	}

	userID := ""
	if consumed.UserID != nil {
		userID = *consumed.UserID
	}
	if userID == "" || (client.IsGroupRestricted && !s.userAllowed(ctx, client, userID)) {
		return nil, &tokenFailure{Code: "access_denied", Status: 403}
	}

	response, err := s.mintTokens(ctx, client, userID, consumed.Scope, consumed.Nonce, consumed.Resource, refreshContext{
		requestID: "device:" + consumed.DeviceCodeHash,
		sid:       consumed.DeviceCodeHash[:16],
		authTime:  timeOf(consumed.ApprovedAt, consumed.CreatedAt),
	})
	if err != nil {
		return nil, serverError()
	}

	s.record(ctx, "oidc_token_issued", map[string]any{
		"client_id": client.ID.String(),
		"user_id":   userID,
		"grant":     "device_code",
	})
	return response, nil
}

// newUserCode mints the prefix plus 7 unambiguous uppercase chars.
func newUserCode() (string, error) {
	out := make([]byte, DeviceUserCodeLength)
	for i := range out {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(userCodeAlphabet))))
		if err != nil {
			return "", fmt.Errorf("oidc: generate user code: %w", err)
		}
		out[i] = userCodeAlphabet[n.Int64()]
	}
	return DeviceUserCodePrefix + string(out), nil
}

// normalizeUserCode trims, uppercases, and strips separators the
// user may have typed.
func normalizeUserCode(raw string) string {
	cleaned := make([]rune, 0, len(raw))
	for _, r := range strings.ToUpper(strings.TrimSpace(raw)) {
		if r == '-' || r == ' ' {
			continue
		}
		cleaned = append(cleaned, r)
	}
	return string(cleaned)
}

func timeOf(primary *time.Time, fallback time.Time) time.Time {
	if primary != nil && !primary.IsZero() {
		return *primary
	}
	return fallback
}
