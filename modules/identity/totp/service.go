package totp

// service.go owns the TOTP lifecycle rules: enrollment, confirmation
// with recovery-code issuance, pending-auth verification, rotation,
// and disablement. No HTTP knowledge lives here.

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/riipandi/tango/internal/kernel"
	"github.com/riipandi/tango/modules/identity"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/modules/identity/token"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
)

// Recovery code shape: eight eight-digit numeric codes per set.
const (
	recoveryCodeCount = 8
	recoveryCodeBytes = 4 // 8 hex characters per code
)

// PendingTTL bounds the pending-auth bridge; the shared TTL lives
// in the identity package (PendingCookieTTL).

// Errors surfaced to handlers.
var (
	// ErrAlreadyConfirmed blocks a second enrollment over a live one.
	ErrAlreadyConfirmed = errors.New("totp: a confirmed enrollment already exists")
	// ErrNoEnrollment covers verification without any enrollment.
	ErrNoEnrollment = errors.New("totp: no enrollment exists")
	// ErrNotConfirmed covers verification against an unconfirmed seed.
	ErrNotConfirmed = errors.New("totp: enrollment is not confirmed")
)

// Sessions are the session operations verification needs; a
// confirmed TOTP upgrades a pending auth into the only fresh session.
type Sessions interface {
	IssueForUser(ctx context.Context, userID user.UserID, provider string, meta session.Meta) (string, session.Session, error)
	IssueAccess(ctx context.Context, p kernel.Principal) (string, time.Time, error)
}

// PasswordVerifier checks the current password for disablement.
type PasswordVerifier interface {
	VerifyForUser(ctx context.Context, userID user.UserID, secret string) error
}

// Service runs the TOTP lifecycle.
type Service struct {
	store    Store
	users    user.Store
	sessions Sessions
	password PasswordVerifier
	cipher   *crypto.Cipher
	recorder identity.Recorder

	// issuer names the relying party in the otpauth link.
	issuer string
	// now is overridable in tests; production uses time.Now.
	now func() time.Time
}

// ServiceOption configures the service.
type ServiceOption func(*Service)

// WithClock overrides the wall clock (tests).
func WithClock(now func() time.Time) ServiceOption {
	return func(s *Service) { s.now = now }
}

// NewService builds the TOTP feature.
func NewService(store Store, users user.Store, sessions Sessions, password PasswordVerifier, cipher *crypto.Cipher, issuer string, recorder identity.Recorder, opts ...ServiceOption) *Service {
	s := &Service{
		store:    store,
		users:    users,
		sessions: sessions,
		password: password,
		cipher:   cipher,
		issuer:   issuer,
		recorder: recorder,
		now:      time.Now,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// BindSessions wires the session issuer after construction; the
// session service needs the MFA port, so the composition root binds
// the two in both directions.
func (s *Service) BindSessions(sessions Sessions) {
	s.sessions = sessions
}

// Name names the feature for logs.
func (s *Service) Name() string { return "totp" }

// Enroll seeds a new enrollment and returns the plaintext secret and
// provisioning URI exactly once. A confirmed enrollment must be
// disabled first.
func (s *Service) Enroll(ctx context.Context, u user.User) (secret, uri string, err error) {
	state, stateErr := s.store.State(ctx, u.ID)
	if stateErr == nil && state.Confirmed() {
		return "", "", ErrAlreadyConfirmed
	}

	secret, err = GenerateSeed()
	if err != nil {
		return "", "", err
	}
	sealed, err := s.cipher.Encrypt(secret)
	if err != nil {
		return "", "", fmt.Errorf("totp: seal seed: %w", err)
	}
	if err := s.store.UpsertUnconfirmed(ctx, u.ID, sealed, defaultDigits, defaultPeriod, AlgorithmSHA1); err != nil {
		return "", "", err
	}
	if s.recorder != nil {
		s.recorder.Record(ctx, identity.AuditEvent{Action: "mfa.totp_enroll_started", Actor: u.ID.String()}, nil)
	}
	return secret, ProvisioningURI(s.issuer, u.Email, secret, defaultDigits, defaultPeriod, AlgorithmSHA1), nil
}

// Confirm verifies one code from the authenticator, marks the
// enrollment live, and returns the recovery codes exactly once.
func (s *Service) Confirm(ctx context.Context, u user.User, code string) ([]string, error) {
	enrollment, err := s.store.State(ctx, u.ID)
	if err != nil {
		return nil, ErrNoEnrollment
	}
	if enrollment.Confirmed() {
		return nil, ErrAlreadyConfirmed
	}

	if _, verifyErr := s.verifySeed(ctx, enrollment, code, false); verifyErr != nil {
		return nil, verifyErr
	}
	if err := s.store.MarkConfirmed(ctx, u.ID, s.now().UTC()); err != nil {
		return nil, err
	}
	// The confirmation is not an authentication: the replay
	// watermark stays untouched so the very same step can still
	// verify the pending sign-in that follows.

	codes, codesErr := s.replaceRecoveryCodes(ctx, u.ID)
	if codesErr != nil {
		return nil, codesErr
	}
	if s.recorder != nil {
		s.recorder.Record(ctx, identity.AuditEvent{Action: "mfa.totp_enabled", Actor: u.ID.String()}, nil)
	}
	return codes, nil
}

// Status reports the enrollment flag and the unspent code count.
func (s *Service) Status(ctx context.Context, u user.User) (confirmed bool, remaining int, err error) {
	enrollment, err := s.store.State(ctx, u.ID)
	if err != nil {
		return false, 0, nil
	}
	remaining, err = s.store.RemainingRecoveryCodes(ctx, u.ID)
	if err != nil {
		return false, 0, err
	}
	return enrollment.Confirmed(), remaining, nil
}

// VerifyPending checks the pending bridge, verifies the TOTP code
// (or burns a recovery code), and issues the only full session. A
// failed attempt leaves the bridge alive: only success consumes it.
func (s *Service) VerifyPending(ctx context.Context, pendingToken, code string) (user.User, string, session.Session, bool, error) {
	userID, remember, err := s.store.PeekPending(ctx, hashToken(pendingToken))
	if err != nil {
		return user.User{}, "", session.Session{}, false, ErrNoEnrollment
	}

	enrollment, err := s.store.State(ctx, userID)
	if err != nil || !enrollment.Confirmed() {
		return user.User{}, "", session.Session{}, false, ErrNotConfirmed
	}

	// A numeric code verifies against the seed; a recovery code
	// burns a stored hash. Wrong answers are indistinguishable.
	if _, verifyErr := s.verifySeed(ctx, enrollment, code, true); verifyErr == nil {
		if delErr := s.store.DeletePending(ctx, hashToken(pendingToken)); delErr != nil {
			return user.User{}, "", session.Session{}, false, delErr
		}
		u, token, se, completeErr := s.complete(ctx, userID, remember)
		return u, token, se, remember, completeErr
	}
	spent, consumeErr := s.store.ConsumeRecoveryCode(ctx, userID, recoveryHash(code))
	if consumeErr != nil || !spent {
		return user.User{}, "", session.Session{}, false, ErrInvalidCode
	}
	if delErr := s.store.DeletePending(ctx, hashToken(pendingToken)); delErr != nil {
		return user.User{}, "", session.Session{}, false, delErr
	}
	u, token, se, completeErr := s.complete(ctx, userID, remember)
	return u, token, se, remember, completeErr
}

// RotateRecoveryCodes verifies one live code and returns a fresh set
// exactly once; the previous set dies immediately.
func (s *Service) RotateRecoveryCodes(ctx context.Context, u user.User, code string) ([]string, error) {
	enrollment, err := s.store.State(ctx, u.ID)
	if err != nil || !enrollment.Confirmed() {
		return nil, ErrNoEnrollment
	}
	if _, verifyErr := s.verifySeed(ctx, enrollment, code, true); verifyErr != nil {
		return nil, verifyErr
	}
	codes, codesErr := s.replaceRecoveryCodes(ctx, u.ID)
	if codesErr != nil {
		return nil, codesErr
	}
	if s.recorder != nil {
		s.recorder.Record(ctx, identity.AuditEvent{Action: "mfa.recovery_codes_rotated", Actor: u.ID.String()}, nil)
	}
	return codes, nil
}

// Disable verifies the current password and drops all MFA state.
func (s *Service) Disable(ctx context.Context, u user.User, currentPassword string) error {
	if err := s.password.VerifyForUser(ctx, u.ID, currentPassword); err != nil {
		return ErrInvalidCode
	}
	if err := s.store.DeleteState(ctx, u.ID); err != nil {
		return err
	}
	if s.recorder != nil {
		s.recorder.Record(ctx, identity.AuditEvent{Action: "mfa.totp_disabled", Actor: u.ID.String()}, nil)
	}
	return nil
}

// RequiresPending implements the identity.MFAPendingIssuer port: a
// confirmed enrollment forces the second factor.
func (s *Service) RequiresPending(ctx context.Context, userID string) (bool, error) {
	parsed, err := identity.ParseID[user.UserID](userID)
	if err != nil {
		return false, nil
	}
	enrollment, err := s.store.State(ctx, parsed)
	if err != nil {
		return false, nil
	}
	return enrollment.Confirmed(), nil
}

// CreatePending implements the identity.MFAPendingIssuer port: it
// replaces the single bridge row and returns the raw token for the
// pending cookie only.
func (s *Service) CreatePending(ctx context.Context, userID string, remember bool) (string, error) {
	parsed, err := identity.ParseID[user.UserID](userID)
	if err != nil {
		return "", err
	}
	raw, err := token.NewRaw()
	if err != nil {
		return "", err
	}
	if err := s.store.PutPending(ctx, parsed, hashToken(raw), identity.PendingCookieTTL, remember); err != nil {
		return "", err
	}
	return raw, nil
}

// ClearPending drops the bridge for the wire-form user ID
// (sign-out); it implements the identity.MFAPendingIssuer port.
func (s *Service) ClearPending(ctx context.Context, userID string) error {
	parsed, err := identity.ParseID[user.UserID](userID)
	if err != nil {
		return nil
	}
	return s.store.DeletePendingForUser(ctx, parsed)
}

// complete issues the only full session for the verified user,
// carrying forward the duration the caller requested before the
// second factor interrupted the sign-in.
func (s *Service) complete(ctx context.Context, userID user.UserID, remember bool) (user.User, string, session.Session, error) {
	token, se, err := s.sessions.IssueForUser(ctx, userID, "totp", session.Meta{Remember: remember})
	if err != nil {
		return user.User{}, "", session.Session{}, err
	}
	u, err := s.users.GetByID(ctx, userID)
	if err != nil {
		return user.User{}, "", session.Session{}, err
	}
	return u, token, se, nil
}

// IssueAccess mints the internal bearer token for the verified user.
func (s *Service) IssueAccess(ctx context.Context, p kernel.Principal) (string, time.Time, error) {
	return s.sessions.IssueAccess(ctx, p)
}

// verifySeed checks one TOTP code against the sealed seed. When
// pinWatermark is set it advances the replay watermark; Confirm runs
// without it so the very same step can still verify the pending
// sign-in that follows.
func (s *Service) verifySeed(ctx context.Context, enrollment Enrollment, code string, pinWatermark bool) (int64, error) {
	seed, err := s.cipher.Decrypt(enrollment.SecretEnc)
	if err != nil {
		return 0, fmt.Errorf("totp: open seed: %w", err)
	}
	var lastStep int64
	if enrollment.LastUsedStep != nil {
		lastStep = *enrollment.LastUsedStep
	} else {
		lastStep = -1
	}
	accepted, err := VerifyCode(seed, code, enrollment.Digits, enrollment.Period, enrollment.Algorithm, defaultSkew, lastStep, s.now())
	if err != nil {
		return 0, err
	}
	if pinWatermark {
		if err := s.store.UpdateLastUsedStep(ctx, enrollment.UserID, accepted); err != nil {
			return 0, err
		}
	}
	return accepted, nil
}

// replaceRecoveryCodes mints a fresh code set; the plaintext values
// return exactly once here.
func (s *Service) replaceRecoveryCodes(ctx context.Context, userID user.UserID) ([]string, error) {
	codes := make([]string, 0, recoveryCodeCount)
	hashes := make([]string, 0, recoveryCodeCount)
	for range recoveryCodeCount {
		buf := make([]byte, recoveryCodeBytes)
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("totp: recovery codes: %w", err)
		}
		code := hex.EncodeToString(buf)
		codes = append(codes, code)
		hashes = append(hashes, recoveryHash(code))
	}
	if err := s.store.ReplaceRecoveryCodes(ctx, userID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// recoveryHash keys the code by its user-scoped SHA-256 hash.
func recoveryHash(code string) string {
	sum := sha256.Sum256([]byte("recovery:" + code))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// hashToken keys the pending-auth bridge by its SHA-256 hash.
func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}
