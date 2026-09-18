package session

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/user"
)

// Service owns the sign-in flow: verify credentials, issue opaque
// tokens, resolve them back to principals, and manage revocation.
type Service struct {
	store    Store
	verifier Verifier
	users    user.Store

	lifetime time.Duration
	// cookieSecure marks the session cookie Secure (HTTPS-only);
	// development runs over plain HTTP.
	cookieSecure bool
	// now is overridable in tests; production uses time.Now.
	now func() time.Time

	recorder identity.Recorder
	// mfa is the optional second-factor port: a confirmed enrollment
	// turns a password sign-in into a pending authentication.
	mfa identity.MFAPendingIssuer
}

// Verifier checks an identity + secret pair. Implemented by the
// password feature; passkeys plug in here later.
type Verifier interface {
	VerifyIdentity(ctx context.Context, identityText, secret string) (user.User, error)
}

// Default session lifetime when none is configured.
const defaultLifetime = 30 * 24 * time.Hour

// ServiceOption configures the session service.
type ServiceOption func(*Service)

// WithRecorder overrides the audit recorder (tests).
func WithRecorder(recorder identity.Recorder) ServiceOption {
	return func(s *Service) { s.recorder = recorder }
}

// WithMFAPort wires the second-factor port; a confirmed enrollment
// makes password sign-in (and every other non-MFA provider) land in
// a pending authentication instead of a full session.
func WithMFAPort(port identity.MFAPendingIssuer) ServiceOption {
	return func(s *Service) { s.mfa = port }
}

// WithLifetime sets the session lifetime and the sliding-refresh
// threshold (half the lifetime).
func WithLifetime(d time.Duration) ServiceOption {
	return func(s *Service) { s.lifetime = d }
}

// WithCookieSecure toggles the Secure cookie flag.
func WithCookieSecure(secure bool) ServiceOption {
	return func(s *Service) { s.cookieSecure = secure }
}

// WithClock overrides the service clock (tests).
func WithClock(now func() time.Time) ServiceOption {
	return func(s *Service) { s.now = now }
}

// NewService builds the session feature.
func NewService(store Store, verifier Verifier, users user.Store, recorder identity.Recorder, opts ...ServiceOption) *Service {
	s := &Service{
		store:    store,
		verifier: verifier,
		users:    users,
		lifetime: defaultLifetime,
		now:      time.Now,
		recorder: recorder,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Name names the feature for logs.
func (s *Service) Name() string { return "session" }

// ErrInvalidCredentials mirrors the password feature error so the
// handler maps both to 401 without importing it.
var ErrInvalidCredentials = errors.New("session: invalid credentials")

// SignIn verifies the identity + secret pair and issues a session
// token. The token goes to the client once (cookie); only its hash
// is stored.
// SignInWithPendingResult carries either a full session or the
// pending-auth bridge that only MFA verification can upgrade.
type SignInWithPendingResult struct {
	Pending bool
	Token   string // pending token when Pending, else the session token
	User    user.User
	Session Session
}

// SignInWithPending verifies the credentials and lands in a full
// session — or in the pending-auth bridge when a confirmed second
// factor requires it. The pending token goes to the pending cookie,
// never to the session cookie.
func (s *Service) SignInWithPending(ctx context.Context, identityText, secret string, meta Meta) (SignInWithPendingResult, error) {
	u, err := s.verifier.VerifyIdentity(ctx, identityText, secret)
	if err != nil {
		return SignInWithPendingResult{}, ErrInvalidCredentials
	}

	if s.mfa != nil {
		required, requiredErr := s.mfa.RequiresPending(ctx, u.ID.String())
		if requiredErr != nil {
			return SignInWithPendingResult{}, requiredErr
		}
		if required {
			pendingToken, pendingErr := s.mfa.CreatePending(ctx, u.ID.String())
			if pendingErr != nil {
				return SignInWithPendingResult{}, pendingErr
			}
			if s.recorder != nil {
				s.recorder.Record(ctx, identity.AuditEvent{Action: "mfa.pending_started", Actor: u.ID.String()}, nil)
			}
			return SignInWithPendingResult{Pending: true, Token: pendingToken, User: u}, nil
		}
	}

	token, se, err := s.issueSession(ctx, u, "password", meta)
	if err != nil {
		return SignInWithPendingResult{}, err
	}
	return SignInWithPendingResult{Token: token, User: u, Session: se}, nil
}

// SignIn verifies the credentials and issues a full session; callers
// that compose with the second factor use SignInWithPending.
func (s *Service) SignIn(ctx context.Context, identityText, secret string, meta Meta) (string, user.User, Session, error) {
	result, err := s.SignInWithPending(ctx, identityText, secret, meta)
	if err != nil {
		return "", user.User{}, Session{}, err
	}
	return result.Token, result.User, result.Session, nil
}

// IssueForUser mints a session for an already-authenticated
// identity — the alternative sign-in providers (passkeys, device
// login, one-time access) call this after verifying their own
// ceremony. A confirmed second factor fails these flows closed: the
// pending bridge belongs to the password sign-in path only, so no
// provider can bypass MFA.
func (s *Service) IssueForUser(ctx context.Context, userID user.UserID, provider string, meta Meta) (string, error) {
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("session: load user: %w", err)
	}
	if u.Disabled {
		return "", fmt.Errorf("session: %w: user is disabled", ErrInvalidCredentials)
	}
	if s.mfa != nil && provider != "totp" {
		required, requiredErr := s.mfa.RequiresPending(ctx, u.ID.String())
		if requiredErr != nil {
			return "", requiredErr
		}
		if required {
			return "", fmt.Errorf("session: %w: second factor required", ErrInvalidCredentials)
		}
	}

	token, _, err := s.issueSession(ctx, u, provider, meta)
	return token, err
}

// issueSession creates the session row + audit trail; returns the
// raw cookie token.
func (s *Service) issueSession(ctx context.Context, u user.User, provider string, meta Meta) (string, Session, error) {
	token, err := newToken()
	if err != nil {
		return "", Session{}, fmt.Errorf("session: token: %w", err)
	}

	now := s.now()
	se := Session{
		ID:        identity.NewID[SessionID]().String(),
		UserID:    u.ID,
		Provider:  provider,
		TokenHash: hashToken(token),
		ExpiresAt: now.Add(s.lifetime),
	}
	applyMeta(&se, meta)

	if err := s.store.Create(ctx, &se); err != nil {
		return "", Session{}, err
	}
	if err := s.users.MarkLogin(ctx, u.ID); err != nil {
		return "", Session{}, fmt.Errorf("session: mark login: %w", err)
	}

	if s.recorder != nil {
		s.recorder.Record(ctx, identity.AuditEvent{Action: "user.signed_in", Actor: u.ID.String(), Target: se.ID}, nil)
	}
	return token, se, nil
}

// Resolve maps a cookie token back to its principal and live
// session, sliding the expiry forward when past half-life.
func (s *Service) Resolve(ctx context.Context, token string) (user.User, Session, error) {
	tokenHash := hashToken(token)
	se, u, err := s.store.ValidByTokenHash(ctx, tokenHash)
	if err != nil {
		return user.User{}, Session{}, err
	}

	// Sliding expiry: refresh once past half-life so active sessions
	// never expire mid-use.
	if remaining := time.Until(se.ExpiresAt); remaining < s.lifetime/2 {
		expiresAt := s.now().Add(s.lifetime)
		if err := s.store.Touch(ctx, se.ID, expiresAt); err != nil {
			return user.User{}, Session{}, err
		}
		se.ExpiresAt = expiresAt
	}
	return u, se, nil
}

// RevokeCurrent signs out the caller's session by token.
func (s *Service) RevokeCurrent(ctx context.Context, token string) error {
	if err := s.store.RevokeByTokenHash(ctx, hashToken(token)); err != nil {
		return err
	}
	if s.recorder != nil {
		s.recorder.Record(ctx, identity.AuditEvent{Action: "user.signed_out"}, nil)
	}
	return nil
}

// ListForUser returns the user's live sessions, newest first.
func (s *Service) ListForUser(ctx context.Context, userID user.UserID) ([]Session, error) {
	return s.store.ListActiveForUser(ctx, userID)
}

// RevokeForUser revokes one session owned by the user; unknown or
// foreign session IDs surface ErrNotFound.
func (s *Service) RevokeForUser(ctx context.Context, userID user.UserID, sessionID string) error {
	return s.store.RevokeForUser(ctx, userID, sessionID)
}

// RevokeAllForUser revokes every session, optionally sparing one
// (the current session during a password change).
func (s *Service) RevokeAllForUser(ctx context.Context, userID user.UserID, keepID string) error {
	return s.store.RevokeAllForUser(ctx, userID, keepID)
}

// ResolveSession implements kernel.Authenticator: cookie token
// in, transport principal out.
func (s *Service) ResolveSession(ctx context.Context, token string) (kernel.Principal, error) {
	u, se, err := s.Resolve(ctx, token)
	if err != nil {
		return kernel.Principal{}, err
	}
	return kernel.Principal{
		SessionID: se.ID,
		UserID:    u.ID.String(),
		Username:  u.Username,
		Email:     u.Email,
		Provider:  se.Provider,
		IsAdmin:   u.IsAdmin,
	}, nil
}

// newToken draws an opaque 256-bit token, base64url-encoded.
func newToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// hashToken derives the stored lookup key for a token.
func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum)
}

// applyMeta copies optional sign-in context, empty → NULL.
func applyMeta(se *Session, meta Meta) {
	if meta.UserAgent != "" {
		se.UserAgent = &meta.UserAgent
	}
	if meta.DeviceName != "" {
		se.DeviceName = &meta.DeviceName
	}
	if meta.IPAddress != "" {
		se.IPAddress = &meta.IPAddress
	}
}
