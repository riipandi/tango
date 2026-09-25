package signup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/go-ozzo/ozzo-validation/v4"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/modules/identity/password"
	"github.com/riipandi/tango/modules/identity/user"
	"github.com/riipandi/tango/pkg/crypto"
)

// The failures a sign-up reports. The handler maps them to connect codes, so
// the wire form of a refusal lives with the transport, not here.
var (
	// ErrInvalidToken covers an unknown, expired, and spent signup token:
	// answering differently would tell a caller which half was wrong.
	ErrInvalidToken = errors.New("signup: invalid signup token")

	// ErrAccountExists covers a taken username and a taken email. The
	// username is matched case-insensitively, the way its unique index is.
	ErrAccountExists = errors.New("signup: account already exists")
)

// usernamePattern is the grammar the users table enforces on the handle:
// 3-32 characters of ASCII letters, digits, and underscores.
var usernamePattern = regexp.MustCompile(`^[a-zA-Z0-9_]{3,32}$`)

// emailPattern is the check the users table places on the address, mirrored
// here so a bad field is refused before the database names it a violation.
var emailPattern = regexp.MustCompile(`^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$`)

// Service creates an account from a signup token.
type Service struct {
	pool   *datastore.Postgres
	repo   *Repository
	hasher *crypto.PasswordHasher
	log    *slog.Logger
	now    func() time.Time
}

// NewService builds the service. The database writes run in one transaction
// the service opens over the pool, so the account, its credential, and the
// token's use commit together or not at all.
func NewService(pool *datastore.Postgres, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		pool:   pool,
		repo:   NewRepository(),
		hasher: crypto.NewPasswordHasher(),
		log:    log,
		now:    time.Now,
	}
}

// Params carries one sign-up attempt.
type Params struct {
	Username  string
	Email     string
	Password  string
	Token     string
	FirstName string
	LastName  string
}

// Validate applies the rules the users table enforces, so a bad field is
// refused before the database is reached. The keys are the snake_case names
// the proto carries, the form a client reads on the wire.
func (p Params) Validate() error {
	errs := validation.Errors{}
	if err := validation.Validate(p.Username, validation.Required, validation.Match(usernamePattern).
		Error("must be 3-32 characters of letters, digits, and underscores")); err != nil {
		errs["username"] = err
	}
	if err := validation.Validate(p.Email, validation.Required, validation.Match(emailPattern).
		Error("must be a valid email address")); err != nil {
		errs["email"] = err
	}
	if err := validation.Validate(p.Password, validation.Required); err != nil {
		errs["password"] = err
	}
	if err := validation.Validate(p.Token, validation.Required); err != nil {
		errs["token"] = err
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// User is the account view a successful sign-up answers with.
type User struct {
	ID          string
	Username    string
	Email       string
	DisplayName string
}

// Signup validates the request, consumes the token, and creates the account.
//
// Email verification is a later procedure: the account is created unverified,
// which is the column's default and needs no code here yet. The password is
// hashed and stored with the account, so the account can sign in immediately.
func (s *Service) Signup(ctx context.Context, params Params) (User, error) {
	params.Username = strings.TrimSpace(params.Username)
	params.Email = strings.TrimSpace(params.Email)
	params.Token = strings.TrimSpace(params.Token)

	if err := params.Validate(); err != nil {
		return User{}, err
	}

	passwordHash, err := s.hasher.Hash(params.Password)
	if err != nil {
		return User{}, fmt.Errorf("signup: hash password: %w", err)
	}
	tokenHash := tokenSHA256(params.Token)

	name := displayName(params.FirstName, params.LastName, params.Username)

	var created User
	err = s.pool.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) error {
		token, findErr := s.repo.FindSignupTokenByHash(ctx, tx, tokenHash)
		if errors.Is(findErr, datastore.ErrNoRows) {
			return ErrInvalidToken
		}
		if findErr != nil {
			return findErr
		}
		if token.ExpiresAt.Before(s.now()) || token.UsageCount >= token.UsageLimit {
			return ErrInvalidToken
		}

		userID, createErr := s.repo.CreateUser(ctx, tx, user.UserSchema{
			Username:    params.Username,
			Email:       params.Email,
			FirstName:   params.FirstName,
			LastName:    params.LastName,
			DisplayName: name,
		})
		if errUniqueViolation(createErr) {
			return ErrAccountExists
		}
		if createErr != nil {
			return fmt.Errorf("signup: create user: %w", createErr)
		}

		if passErr := s.repo.CreatePassword(ctx, tx, password.UserPasswordSchema{
			UserID:       userID,
			PasswordHash: passwordHash,
		}); passErr != nil {
			return fmt.Errorf("signup: create password: %w", passErr)
		}

		if consumeErr := s.repo.ConsumeSignupToken(ctx, tx, token.ID, s.now()); consumeErr != nil {
			return consumeErr
		}

		created = User{
			ID:          userID.String(),
			Username:    params.Username,
			Email:       params.Email,
			DisplayName: name,
		}
		return nil
	})
	if err != nil {
		return User{}, err
	}
	return created, nil
}

// displayName composes the name the UI shows from the optional given and
// family names, falling back to the username when neither is carried.
func displayName(firstName, lastName, username string) string {
	name := strings.TrimSpace(strings.Join([]string{strings.TrimSpace(firstName), strings.TrimSpace(lastName)}, " "))
	if name == "" {
		return username
	}
	return name
}

// tokenSHA256 hashes the raw token the caller presented, the form the
// signup_tokens table stores.
func tokenSHA256(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
