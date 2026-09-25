package signin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/lestrrat-go/jwx/v3/jwa"
	"github.com/lestrrat-go/jwx/v3/jwk"
	"go.jetify.com/typeid"

	"github.com/riipandi/tango/internal/config"
	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/jwks"
	"github.com/riipandi/tango/modules/identity/session"
	"github.com/riipandi/tango/pkg/crypto"
	"github.com/riipandi/tango/pkg/jwtutils"
)

// The failures a sign-in reports. The handler maps them to connect codes, so
// the wire form of a refusal lives with the transport, not here.
var (
	// ErrInvalidCredentials covers both a missing account and a wrong
	// password: answering differently would tell a caller which half was
	// wrong and turn the endpoint into an account enumerator.
	ErrInvalidCredentials = errors.New("signin: invalid credentials")

	// ErrAccountDisabled is an account switched off by an operator.
	ErrAccountDisabled = errors.New("signin: account is disabled")

	// ErrAccountBanned is an account inside its ban window. The reason is
	// not attached: it is operator-facing material, not a wire detail.
	ErrAccountBanned = errors.New("signin: account is banned")
)

// TokenType is the authorization scheme the access token is presented under.
const TokenType = "Bearer"

// refreshTokenBytes is the entropy of a refresh token. The token is shown to
// the caller once; only its hash is stored, so 256 bits of randomness is the
// whole defense against a database leak.
const refreshTokenBytes = 32

// Service verifies the primary credential and issues the token pair.
type Service struct {
	repo      *Repository
	keys      *jwks.Service
	hasher    *crypto.PasswordHasher
	log       *slog.Logger
	issuer    string
	accessTTL time.Duration
	shortTTL  time.Duration
	longTTL   time.Duration
	now       func() time.Time

	// dummyHash holds the hash a sign-in of an unknown account is checked
	// against, so the two failure paths cost the same work. It is computed
	// once, on the first miss.
	dummyHash once[string]
}

// NewService builds the service. keys is the area's key-set service: the
// signing material resolves through it, so the dual stack (key pair or HMAC
// secret) is the deployment's decision, not this feature's.
func NewService(cfg config.Config, repo *Repository, keys *jwks.Service, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		repo:      repo,
		keys:      keys,
		hasher:    crypto.NewPasswordHasher(),
		log:       log,
		issuer:    cfg.Auth.Issuer,
		accessTTL: cfg.Auth.AccessTTL,
		shortTTL:  cfg.Auth.RefreshShortTTL,
		longTTL:   cfg.Auth.RefreshLongTTL,
		now:       time.Now,
	}
}

// Params carries one sign-in attempt. IPAddress is the caller's address as
// the transport read it, empty when it is unknown.
type Params struct {
	Identity  string
	Password  string
	Remember  bool
	UserAgent string
	IPAddress string
}

// User is the account view a successful sign-in answers with.
type User struct {
	ID          string
	Username    string
	Email       string
	DisplayName string
	IsAdmin     bool
}

// Result is the token pair and the account it was issued for.
type Result struct {
	AccessToken  string
	TokenType    string
	ExpiresIn    int32
	RefreshToken string
	SessionID    string
	User         User
}

// SignIn verifies the credential and issues the access and refresh tokens.
//
// The refresh token is the only server-side state: a hashed row in the
// sessions table, so a later procedure can rotate and revoke it without the
// access token ever depending on a lookup.
func (s *Service) SignIn(ctx context.Context, params Params) (Result, error) {
	now := s.now()

	account, err := s.repo.FindAccountByIdentity(ctx, params.Identity)
	if errors.Is(err, datastore.ErrNoRows) {
		// The same cost runs here as on a password mismatch, so the
		// response time does not disclose whether the account exists.
		s.verifyDummy(params.Password)
		return Result{}, ErrInvalidCredentials
	}
	if err != nil {
		return Result{}, err
	}

	match, verifyErr := s.hasher.Verify(params.Password, account.PasswordHash)
	if verifyErr != nil {
		// A stored hash that cannot be read is an account that can never
		// sign in; the answer to the caller is the same as a mismatch, and
		// the log carries the difference.
		s.log.ErrorContext(ctx, "signin: unreadable password hash", "error", verifyErr)
		return Result{}, ErrInvalidCredentials
	}
	if !match {
		return Result{}, ErrInvalidCredentials
	}

	switch {
	case account.Disabled:
		return Result{}, ErrAccountDisabled
	case bannedAt(account, now):
		return Result{}, ErrAccountBanned
	}

	refresh, err := newRefreshToken()
	if err != nil {
		return Result{}, fmt.Errorf("signin: refresh token: %w", err)
	}

	sessionID, err := typeid.New[session.SessionID]()
	if err != nil {
		return Result{}, fmt.Errorf("signin: session id: %w", err)
	}
	sessionRow := session.SessionSchema{
		ID:        sessionID,
		UserID:    account.ID,
		Provider:  ProviderPassword,
		TokenHash: refresh.hash,
		UserAgent: params.UserAgent,
		IPAddress: addrPtr(params.IPAddress),
		Remember:  params.Remember,
		CreatedAt: now,
		ExpiresAt: now.Add(s.sessionTTL(params.Remember)),
	}
	if createErr := s.repo.CreateSession(ctx, sessionRow); createErr != nil {
		return Result{}, createErr
	}
	if touchErr := s.repo.TouchLastLogin(ctx, account.ID, now); touchErr != nil {
		return Result{}, touchErr
	}

	access, err := s.signAccess(ctx, account, sessionID, now)
	if err != nil {
		return Result{}, err
	}

	return Result{
		AccessToken:  access,
		TokenType:    TokenType,
		ExpiresIn:    int32(s.accessTTL.Seconds()),
		RefreshToken: refresh.plain,
		SessionID:    sessionID.String(),
		User: User{
			ID:          account.ID.String(),
			Username:    account.Username,
			Email:       account.Email,
			DisplayName: account.DisplayName,
			IsAdmin:     account.IsAdmin,
		},
	}, nil
}

// bannedAt reports whether the account sits inside its ban window. A ban
// without an expiry never lifts by itself.
func bannedAt(account *Account, at time.Time) bool {
	return account.BannedAt != nil && (account.BanExpires == nil || account.BanExpires.After(at))
}

// sessionTTL picks the session lifetime the caller asked for: the short
// window a shared machine forgets by the end of the day, the long one a
// remembered device keeps. Both are configuration keys, so a deployment
// decides the two windows.
func (s *Service) sessionTTL(remember bool) time.Duration {
	if remember {
		return s.longTTL
	}
	return s.shortTTL
}

// signAccess mints the stateless token. The key and algorithm resolve on
// every call rather than at construction, so a rotation is picked up without
// a restart.
func (s *Service) signAccess(ctx context.Context, account *Account, sessionID session.SessionID, now time.Time) (string, error) {
	algorithm, err := s.keys.SigningAlgorithm()
	if err != nil {
		return "", fmt.Errorf("signin: signing algorithm: %w", err)
	}
	key, err := s.signingKey(ctx, algorithm)
	if err != nil {
		return "", fmt.Errorf("signin: signing key: %w", err)
	}

	signer, err := jwtutils.NewSigner[jwtutils.AccessClaims](key, algorithm)
	if err != nil {
		return "", fmt.Errorf("signin: signer: %w", err)
	}
	signer = signer.WithIssuer(s.issuer).WithTTL(s.accessTTL)

	token, err := signer.Sign(jwtutils.AccessClaims{
		Email:       account.Email,
		Username:    account.Username,
		DisplayName: account.DisplayName,
		IsAdmin:     account.IsAdmin,
		SessionID:   sessionID.String(),
	}, jwtutils.Standard{
		Subject:   account.ID.String(),
		IssuedAt:  now,
		NotBefore: now,
	})
	if err != nil {
		return "", fmt.Errorf("signin: sign access token: %w", err)
	}
	return token, nil
}

// signingKey picks the half of the dual stack the resolved algorithm names: a
// symmetric algorithm signs with the HMAC secret, anything else with the
// configured key pair.
func (s *Service) signingKey(ctx context.Context, algorithm jwa.SignatureAlgorithm) (jwk.Key, error) {
	if algorithm.IsSymmetric() {
		return s.keys.HMACKey(ctx)
	}
	return s.keys.SignKey(ctx)
}

type tokenPair struct {
	plain string
	hash  string
}

// newRefreshToken draws the token the caller sees and returns it beside the
// hash the session row stores.
func newRefreshToken() (tokenPair, error) {
	raw := make([]byte, refreshTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return tokenPair{}, err
	}
	plain := base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(plain))
	return tokenPair{plain: plain, hash: hex.EncodeToString(sum[:])}, nil
}

// verifyDummy runs the password verifier against a hash of nothing, so a
// sign-in of an unknown account does the same hashing work as a wrong
// password and the two failures are indistinguishable by timing.
func (s *Service) verifyDummy(password string) {
	hash, ok := s.dummyHash.get(func() (string, error) {
		return s.hasher.Hash("tango-dummy-account")
	})
	if ok {
		match, _ := s.hasher.Verify(password, hash)
		_ = match
	}
}

// once computes a value at most once; a failure is retried on the next call.
type once[T any] struct {
	mu    sync.Mutex
	value T
	done  bool
}

func (o *once[T]) get(fn func() (T, error)) (T, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if !o.done {
		value, err := fn()
		if err != nil {
			return o.value, false
		}
		o.value, o.done = value, true
	}
	return o.value, true
}

// addrPtr converts the caller address to the INET column's form, nil when the
// transport did not supply one.
func addrPtr(raw string) *netip.Addr {
	addr, err := netip.ParseAddr(strings.TrimSpace(raw))
	if err != nil {
		return nil
	}
	return &addr
}
