package oidc

// device.go implements the OAuth 2.0 Device Authorization Grant
// (RFC 8628): a limited-input device obtains a device code and user
// code, the user approves in a browser, and the device polls the
// token endpoint. Distinct from the identity devicelogin surface.

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/riipandi/tango/pkg/responder"
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

// HandleDeviceAuthorize implements POST /api/oidc/device/authorize
// (client-authenticated, form-encoded). Bare OAuth errors: the
// caller is a device, not a browser.
func (s *Service) HandleDeviceAuthorize(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}

	client, ok := s.authenticateClient(w, r)
	if !ok {
		return // response already written
	}

	scope := r.PostFormValue("scope")
	if !scopeList(scope)[ScopeOpenID] {
		tokenError(w, r, "invalid_scope", http.StatusBadRequest)
		return
	}

	deviceCode, err := randomToken()
	if err != nil {
		tokenError(w, r, "server_error", http.StatusInternalServerError)
		return
	}
	userCode, err := newUserCode()
	if err != nil {
		tokenError(w, r, "server_error", http.StatusInternalServerError)
		return
	}

	code := DeviceCode{
		DeviceCodeHash: sha256Hex(deviceCode),
		UserCodeHash:   sha256Hex(normalizeUserCode(userCode)),
		Scope:          scope,
		Resource:       r.PostFormValue("resource"),
		Nonce:          r.PostFormValue("nonce"),
		ClientID:       client.ID.String(),
		Status:         DeviceStatusPending,
		ExpiresAt:      time.Now().UTC().Add(DeviceCodeTTL),
	}
	if err := s.store.InsertDeviceCode(r.Context(), code); err != nil {
		tokenError(w, r, "server_error", http.StatusInternalServerError)
		return
	}

	s.record(r.Context(), "oidc_device_authorization_created", map[string]any{
		"client_id": client.ID.String(),
	})
	responder.WriteJSON(w, http.StatusOK, deviceAuthorizationResponse{
		DeviceCode:              deviceCode,
		UserCode:                userCode,
		VerificationURI:         s.issuer + DeviceVerificationPath,
		VerificationURIComplete: s.issuer + DeviceVerificationPath + "?code=" + userCode,
		ExpiresIn:               int(DeviceCodeTTL.Seconds()),
		Interval:                DevicePollInterval,
	})
}

// HandleDeviceVerify implements POST /api/oidc/device/verify: the
// browser session approves or denies the pending authorization. 204
// on success; the SPA re-checks /device/info for the consent state.
func (s *Service) HandleDeviceVerify(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid request")
		return
	}
	userCode := r.PostFormValue("code")
	if userCode == "" {
		responder.Fail(w, r, http.StatusBadRequest, "missing code")
		return
	}

	principal, authenticated := s.principal(r)
	if !authenticated {
		responder.Fail(w, r, http.StatusUnauthorized, "authentication required")
		return
	}

	hash := sha256Hex(normalizeUserCode(userCode))
	code, err := s.store.GetDeviceCodeByUserCode(r.Context(), hash)
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid user code")
		return
	}
	if code.ExpiresAt.Before(time.Now().UTC()) {
		responder.Fail(w, r, http.StatusBadRequest, "device code expired")
		return
	}
	if code.Status != DeviceStatusPending {
		// A code may be approved only once: no rebinding to a second
		// user before the device polls.
		responder.Fail(w, r, http.StatusConflict, "device code already decided")
		return
	}

	client, err := s.store.GetClient(r.Context(), OIDCClientIDFromClientKey(code.ClientID))
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid user code")
		return
	}
	if client.IsGroupRestricted && !s.userAllowed(r.Context(), client, principal.UserID) {
		responder.Fail(w, r, http.StatusForbidden, "user is not allowed to use this client")
		return
	}

	if r.PostFormValue("action") == "deny" {
		if err := s.store.DenyDeviceCode(r.Context(), hash); err != nil {
			responder.Fail(w, r, http.StatusInternalServerError, "failed to deny device code")
			return
		}
		s.record(r.Context(), "oidc_device_authorization_denied", map[string]any{
			"client_id": client.ID.String(),
			"user_id":   principal.UserID,
		})
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// The RFC 8707 resource resolves at approval time: the granted
	// audience and scope subset are what the poll will mint.
	audience, granted, errName := s.resolveResource(r.Context(), client.ID.String(), code.Resource, code.Scope, SubjectUser)
	if errName != "" {
		responder.Fail(w, r, http.StatusForbidden, "resource indicator is not allowed for this client")
		return
	}
	if err := s.store.ApproveDeviceCode(r.Context(), hash, principal.UserID); err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to approve device code")
		return
	}
	if err := s.store.UpdateDeviceCodeGrant(r.Context(), hash, audience, strings.Join(granted, " ")); err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to record the grant")
		return
	}
	if err := s.store.UpsertAuthorizedClient(r.Context(), principal.UserID, client.ID.String(), granted); err != nil {
		responder.Fail(w, r, http.StatusInternalServerError, "failed to record consent")
		return
	}
	s.record(r.Context(), "oidc_device_authorization_approved", map[string]any{
		"client_id": client.ID.String(),
		"user_id":   principal.UserID,
	})
	w.WriteHeader(http.StatusNoContent)
}

// HandleDeviceInfo implements GET /api/oidc/device/info: what the
// consent page shows for a user code.
func (s *Service) HandleDeviceInfo(w http.ResponseWriter, r *http.Request) {
	userCode := r.URL.Query().Get("code")
	if userCode == "" {
		responder.Fail(w, r, http.StatusBadRequest, "missing code")
		return
	}

	hash := sha256Hex(normalizeUserCode(userCode))
	code, err := s.store.GetDeviceCodeByUserCode(r.Context(), hash)
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid user code")
		return
	}
	if code.ExpiresAt.Before(time.Now().UTC()) {
		responder.Fail(w, r, http.StatusBadRequest, "device code expired")
		return
	}

	client, err := s.store.GetClient(r.Context(), OIDCClientIDFromClientKey(code.ClientID))
	if err != nil {
		responder.Fail(w, r, http.StatusBadRequest, "invalid user code")
		return
	}
	responder.WriteJSON(w, http.StatusOK, deviceCodeInfo{
		ClientID:   client.ID.String(),
		ClientName: client.Name,
		Scope:      code.Scope,
	})
}

// exchangeDevice implements the urn:ietf:params:oauth:grant-type:
// device_code grant: poll until approved. Error codes follow RFC
// 8628 §3.5 — authorization_pending, slow_down, expired_token.
func (s *Service) exchangeDevice(w http.ResponseWriter, r *http.Request, client Client) {
	ctx := r.Context()

	deviceCode := r.PostFormValue("device_code")
	if deviceCode == "" {
		tokenError(w, r, "invalid_request", http.StatusBadRequest)
		return
	}

	hash := sha256Hex(deviceCode)
	code, err := s.store.GetDeviceCode(ctx, hash)
	if err != nil || code.ClientID != client.ID.String() {
		tokenError(w, r, "invalid_grant", http.StatusBadRequest)
		return
	}

	now := time.Now().UTC()

	// Final states answer directly; slow-down discipline only
	// governs the pending wait.
	switch code.Status {
	case DeviceStatusDenied:
		tokenError(w, r, "access_denied", http.StatusForbidden)
		return
	case DeviceStatusApproved:
		s.consumeDeviceToken(ctx, w, r, client, hash, code)
		return
	}

	// Pending: poll faster than the advertised interval and the wait
	// escalates (§3.5 slow_down); an expired authorization dies.
	if code.ExpiresAt.Before(now) {
		tokenError(w, r, "expired_token", http.StatusBadRequest)
		return
	}
	if code.LastPolledAt != nil && now.Sub(*code.LastPolledAt) < DevicePollInterval*time.Second {
		_ = s.store.TouchDevicePoll(ctx, hash, now)
		tokenError(w, r, "slow_down", http.StatusBadRequest)
		return
	}
	_ = s.store.TouchDevicePoll(ctx, hash, now)
	tokenError(w, r, "authorization_pending", http.StatusBadRequest)
}

// consumeDeviceToken flips the approved authorization to consumed
// and mints the token response.
func (s *Service) consumeDeviceToken(ctx context.Context, w http.ResponseWriter, r *http.Request, client Client, hash string, code DeviceCode) {
	consumed, err := s.store.ConsumeDeviceCode(ctx, hash)
	if err != nil {
		tokenError(w, r, "expired_token", http.StatusBadRequest)
		return
	}

	userID := ""
	if consumed.UserID != nil {
		userID = *consumed.UserID
	}
	if userID == "" || (client.IsGroupRestricted && !s.userAllowed(ctx, client, userID)) {
		tokenError(w, r, "access_denied", http.StatusForbidden)
		return
	}

	response, err := s.mintTokens(ctx, client, userID, consumed.Scope, consumed.Nonce, consumed.Resource, refreshContext{
		requestID: "device:" + consumed.DeviceCodeHash,
		sid:       consumed.DeviceCodeHash[:16],
		authTime:  timeOf(consumed.ApprovedAt, consumed.CreatedAt),
	})
	if err != nil {
		tokenError(w, r, "server_error", http.StatusInternalServerError)
		return
	}

	s.record(ctx, "oidc_token_issued", map[string]any{
		"client_id": client.ID.String(),
		"user_id":   userID,
		"grant":     "device_code",
	})
	responder.WriteJSON(w, http.StatusOK, response)
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
