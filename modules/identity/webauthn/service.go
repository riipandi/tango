package webauthn

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	gowebauthn "github.com/go-webauthn/webauthn/webauthn"

	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
)

// Service implements the passkey ceremonies. Login is discoverable
// (userless): the handle selects the user.
type Service struct {
	store        Store
	users        user.Store
	issuer       SessionIssuer
	webAuthn     *gowebauthn.WebAuthn
	recorder     identity.Recorder
	cookieName   string
	cookieSecure bool
}

// SessionIssuer issues a sign-in session after a successful
// assertion — implemented by the session feature via the registry.
type SessionIssuer func(ctx context.Context, userID user.UserID) (string, error)

// NewService builds the passkey feature. appURL anchors the RP ID
// and permitted origin.
func NewService(store Store, users user.Store, sessions SessionIssuer, appURL string, recorder identity.Recorder, opts ...ServiceOption) (*Service, error) {
	wa, err := gowebauthn.New(&gowebauthn.Config{
		RPDisplayName: "Tango",
		RPID:          hostnameOf(appURL),
		RPOrigins:     []string{appURL},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationRequired,
		},
		Timeouts: gowebauthn.TimeoutsConfig{
			Login:        gowebauthn.TimeoutConfig{Enforce: true, Timeout: ChallengeSessionTTL},
			Registration: gowebauthn.TimeoutConfig{Enforce: true, Timeout: ChallengeSessionTTL},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("webauthn: init: %w", err)
	}

	s := &Service{store: store, users: users, issuer: sessions, webAuthn: wa, recorder: recorder}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

// ServiceOption configures the passkey feature.
type ServiceOption func(*Service)

// WithCookieSecure mirrors the session cookie Secure flag.
func WithCookieSecure(secure bool) ServiceOption {
	return func(s *Service) { s.cookieSecure = secure }
}

// WithCookieName sets the session cookie name for the sign-in
// response.
func WithCookieName(name string) ServiceOption {
	return func(s *Service) { s.cookieName = name }
}

// Name names the feature for logs.
func (s *Service) Name() string { return "webauthn" }

// BeginRegistration starts the attestation ceremony for a user.
func (s *Service) BeginRegistration(ctx context.Context, userID user.UserID) (*protocol.PublicKeyCredentialCreationOptions, string, error) {
	waUser, err := s.loadUser(ctx, userID)
	if err != nil {
		return nil, "", err
	}

	var exclusions []protocol.CredentialDescriptor
	stored, listErr := s.store.ListCredentials(ctx, userID)
	if listErr == nil {
		for _, row := range stored {
			exclusions = append(exclusions, descriptor(row))
		}
	}

	options, session, err := s.webAuthn.BeginRegistration(
		waUser,
		gowebauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		gowebauthn.WithExclusions(exclusions),
	)
	if err != nil {
		return nil, "", fmt.Errorf("webauthn: begin registration: %w", err)
	}

	sessionID, err := s.saveSession(ctx, &userID, session, ChallengeRegistration)
	if err != nil {
		return nil, "", err
	}
	return &options.Response, sessionID, nil
}

// PrepareRegistration loads the user and consumes the registration
// ceremony session (one-time); the caller then runs the protocol
// ceremony against the browser response and hands the credential to
// StoreRegistration.
func (s *Service) PrepareRegistration(ctx context.Context, sessionID string, userID user.UserID) (*User, gowebauthn.SessionData, error) {
	waUser, err := s.loadUser(ctx, userID)
	if err != nil {
		return nil, gowebauthn.SessionData{}, err
	}

	session, err := s.consumeSession(ctx, sessionID, ChallengeRegistration)
	if err != nil {
		return nil, gowebauthn.SessionData{}, err
	}
	return waUser, session, nil
}

// StoreRegistration persists a completed attestation result.
func (s *Service) StoreRegistration(ctx context.Context, userID user.UserID, credential *gowebauthn.Credential) (StoredCredential, error) {
	row := StoredCredential{
		UserID:          userID,
		Name:            "New Passkey",
		CredentialID:    credential.ID,
		PublicKey:       credential.PublicKey,
		DeviceType:      string(credential.Authenticator.Attachment),
		AttestationType: credential.AttestationType,
		BackupEligible:  credential.Flags.BackupEligible,
		BackupState:     credential.Flags.BackupState,
		AAGUID:          aaguidHex(credential.Authenticator.AAGUID),
	}
	for _, t := range credential.Transport {
		row.Transport = append(row.Transport, string(t))
	}
	if err := s.store.InsertCredential(ctx, &row); err != nil {
		return StoredCredential{}, err
	}

	s.record(ctx, "passkey.added", userID.String())
	return row, nil
}

// BeginLogin starts a discoverable assertion ceremony (userless).
func (s *Service) BeginLogin(ctx context.Context) (*protocol.PublicKeyCredentialRequestOptions, string, error) {
	options, session, err := s.webAuthn.BeginDiscoverableLogin()
	if err != nil {
		return nil, "", fmt.Errorf("webauthn: begin login: %w", err)
	}

	sessionID, err := s.saveSession(ctx, nil, session, ChallengeAuthentication)
	if err != nil {
		return nil, "", err
	}
	return &options.Response, sessionID, nil
}

// VerifyLogin finishes the discoverable assertion: consumes the
// session, resolves the user by handle, issues the sign-in session.
func (s *Service) VerifyLogin(ctx context.Context, sessionID string, parsedResponse *protocol.ParsedCredentialAssertionData) (user.User, string, error) {
	session, err := s.consumeSession(ctx, sessionID, ChallengeAuthentication)
	if err != nil {
		return user.User{}, "", err
	}

	var asserted *User
	_, err = s.webAuthn.ValidateDiscoverableLogin(func(_, handle []byte) (gowebauthn.User, error) {
		resolved, resolveErr := s.loadUser(ctx, user.MustID(string(handle)))
		if resolveErr != nil {
			return nil, resolveErr
		}
		asserted = resolved
		return resolved, nil
	}, session, parsedResponse)
	if err != nil {
		return user.User{}, "", classifyAssertionError(err)
	}
	if asserted == nil || asserted.u.Disabled {
		return user.User{}, "", ErrNotFound
	}

	userID := asserted.u.ID
	token, err := s.sessions(ctx, userID)
	if err != nil {
		return user.User{}, "", err
	}
	s.record(ctx, "user.signed_in", userID.String())
	return asserted.u, token, nil
}

// sessions issues the sign-in session via the injected issuer.
func (s *Service) sessions(ctx context.Context, userID user.UserID) (string, error) {
	if s.issuer == nil {
		return "", errors.New("webauthn: no session issuer wired")
	}
	return s.issuer(ctx, userID)
}

// ListCredentials returns a user's passkeys.
func (s *Service) ListCredentials(ctx context.Context, userID user.UserID) ([]StoredCredential, error) {
	return s.store.ListCredentials(ctx, userID)
}

// DeleteCredential removes one passkey.
func (s *Service) DeleteCredential(ctx context.Context, userID user.UserID, credentialID CredentialID) error {
	found, err := s.store.DeleteCredential(ctx, userID, credentialID)
	if err != nil {
		return err
	}
	if !found {
		return ErrNotFound
	}
	s.record(ctx, "passkey.removed", userID.String())
	return nil
}

// RenameCredential updates the passkey display name.
func (s *Service) RenameCredential(ctx context.Context, userID user.UserID, credentialID CredentialID, name string) (*StoredCredential, error) {
	return s.store.RenameCredential(ctx, userID, credentialID, name)
}

// loadUser wraps the identity user with its stored credentials.
func (s *Service) loadUser(ctx context.Context, userID user.UserID) (*User, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("webauthn: load user: %w", err)
	}

	stored, err := s.store.ListCredentials(ctx, userID)
	if err != nil {
		return nil, err
	}
	credentialSet := make([]gowebauthn.Credential, 0, len(stored))
	for _, row := range stored {
		credentialSet = append(credentialSet, toLibraryCredential(row))
	}
	return NewUser(u, credentialSet), nil
}

// saveSession persists the ceremony state with a TTL.
func (s *Service) saveSession(ctx context.Context, userID *user.UserID, session *gowebauthn.SessionData, kind string) (string, error) {
	row := ChallengeSession{
		Challenge:    session.Challenge,
		Type:         kind,
		Verification: string(session.UserVerification),
		CredParams:   []byte("[]"),
		Extensions:   []byte("{}"),
		ExpiresAt:    time.Now().UTC().Add(ChallengeSessionTTL),
	}
	if userID != nil {
		// The column is a bare UUID; the TypeID form is wire-only.
		owner := userID.UUID()
		row.UserID = &owner
	}

	sessionID, err := s.store.SaveChallengeSession(ctx, row)
	if err != nil {
		return "", err
	}
	return sessionID, nil
}

// consumeSession deletes the ceremony row, enforcing the type.
func (s *Service) consumeSession(ctx context.Context, id, kind string) (gowebauthn.SessionData, error) {
	row, err := s.store.ConsumeSessionByID(ctx, id)
	if err != nil {
		return gowebauthn.SessionData{}, err
	}
	if row.Type != kind {
		return gowebauthn.SessionData{}, ErrInvalidSession
	}
	return gowebauthn.SessionData{
		Challenge:        row.Challenge,
		Expires:          row.ExpiresAt,
		UserVerification: protocol.UserVerificationRequirement(row.Verification),
	}, nil
}

// record emits an audit event through the adapter.
func (s *Service) record(ctx context.Context, action, actor string) {
	if s.recorder != nil {
		s.recorder.Record(ctx, identity.AuditEvent{Action: action, Actor: actor}, nil)
	}
}

// aaguidHex renders the AAGUID bytes as the canonical UUID string.
func aaguidHex(raw []byte) *string {
	if len(raw) != 16 {
		return nil
	}
	h := hex.EncodeToString(raw)
	formatted := h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
	return &formatted
}

// hostnameOf extracts the RP ID (hostname) from the app URL.
func hostnameOf(appURL string) string {
	if parsed, err := url.Parse(appURL); err == nil && parsed.Host != "" {
		return parsed.Hostname()
	}
	return appURL
}

// classifyAssertionError maps library failures to domain errors.
func classifyAssertionError(err error) error {
	var protocolErr *protocol.Error
	if errors.As(err, &protocolErr) &&
		protocolErr.Type == protocol.ErrVerification.Type {
		return ErrVerificationRequired
	}
	return ErrAssertionFailed
}
