package verification

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/jobs"
	"github.com/riipandi/tango/internal/mailer"
	"github.com/riipandi/tango/internal/queue"
)

// The failures the flow reports. The handler maps them to connect codes, so
// the wire form of a refusal lives with the transport, not here.
var (
	// ErrUserNotFound is a request whose claims name no account — an account
	// deleted after the token was signed.
	ErrUserNotFound = errors.New("verification: account not found")

	// ErrAlreadyVerified is a request for an account whose address the
	// database already records as verified.
	ErrAlreadyVerified = errors.New("verification: email already verified")

	// ErrMailUnavailable is a request the running process cannot serve: no
	// SMTP host is configured, so enqueuing would only dead-letter a task.
	ErrMailUnavailable = errors.New("verification: mailer is not configured")

	// ErrInvalidToken covers an unknown, expired, and spent verification
	// token: answering differently would tell a caller which half was wrong.
	ErrInvalidToken = errors.New("verification: invalid verification token")

	// ErrResendTooSoon is a request inside the cooldown the last send
	// opened: another message now would only invite a mailbomb.
	ErrResendTooSoon = errors.New("verification: a message was sent recently")
)

// tokenTTL is how long a verification link works. The template copy states
// it, so changing one means changing the other.
const tokenTTL = time.Hour

// resendCooldown is how long the last send keeps a new one out. The window
// is what stops a caller from turning the procedure into a mailbomb; the
// token row's send time is the clock it reads.
const resendCooldown = time.Minute

// tokenEntropy is the randomness of a raw verification token. It is sent to
// one address once and only its hash is stored, so 256 bits is the whole
// defense against a database leak.
const tokenEntropy = 32

// Service issues the verification tokens and consumes them.
type Service struct {
	pool    *datastore.Postgres
	repo    *Repository
	mail    *mailer.Service
	queue   *queue.Client
	baseURL string
	log     *slog.Logger
	now     func() time.Time
}

// NewService builds the service. The mailer and the queue are the
// infrastructure the composition root resolves: the procedure writes the
// token row and enqueues, the queue owns the SMTP attempt and its retries.
func NewService(pool *datastore.Postgres, mail *mailer.Service, client *queue.Client, baseURL string, log *slog.Logger) *Service {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Service{
		pool:    pool,
		repo:    NewRepository(),
		mail:    mail,
		queue:   client,
		baseURL: baseURL,
		log:     log,
		now:     time.Now,
	}
}

// SendEmail issues a verification token for the signed-in account and
// enqueues the message. The account is looked up from the username the
// claims carry — the address on record is the one the message goes to,
// never one the request could name — so a changed address is honored at
// the next request without touching the signed token.
func (s *Service) SendEmail(ctx context.Context, username string) error {
	account, err := s.repo.FindUserByUsername(ctx, s.pool, username)
	if errors.Is(err, datastore.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return err
	}
	if account.EmailVerifiedAt != nil {
		return ErrAlreadyVerified
	}
	if !s.mail.Configured() {
		return ErrMailUnavailable
	}

	// The resend cooldown reads the send time the last token row stamps: a
	// request inside the window refuses, so the resend is the deliberate
	// re-issue the caller waits for, not the loop a script runs.
	if existing, findErr := s.repo.FindTokenByUser(ctx, s.pool, account.ID); findErr == nil && existing.LastSentAt != nil {
		if s.now().Before(existing.LastSentAt.Add(resendCooldown)) {
			return ErrResendTooSoon
		}
	}

	raw := make([]byte, tokenEntropy)
	if _, err := rand.Read(raw); err != nil {
		return fmt.Errorf("verification: token: %w", err)
	}
	rawToken := base64.RawURLEncoding.EncodeToString(raw)
	now := s.now()

	if err := s.repo.UpsertToken(ctx, s.pool, account.ID, tokenSHA256(rawToken), now.Add(tokenTTL), now); err != nil {
		return err
	}
	if _, err := s.queue.Add(jobs.EmailVerificationTask{
		UserID:      account.ID.String(),
		Email:       account.Email,
		DisplayName: account.DisplayName,
		Token:       rawToken,
	}).Save(); err != nil {
		return fmt.Errorf("verification: enqueue: %w", err)
	}
	return nil
}

// VerifyEmail consumes the token and verifies the account. The read, the
// stamp, and the delete run in one transaction, so a token cannot verify
// twice under a race: the delete is what a second caller loses.
func (s *Service) VerifyEmail(ctx context.Context, rawToken string) error {
	err := s.pool.WithTx(ctx, func(ctx context.Context, tx datastore.Querier) error {
		token, findErr := s.repo.FindTokenByHash(ctx, tx, tokenSHA256(rawToken))
		if errors.Is(findErr, datastore.ErrNoRows) {
			return ErrInvalidToken
		}
		if findErr != nil {
			return findErr
		}
		if !token.ExpiresAt.After(s.now()) {
			return ErrInvalidToken
		}

		if markErr := s.repo.MarkVerified(ctx, tx, token.UserID, s.now()); markErr != nil {
			return markErr
		}
		return s.repo.DeleteToken(ctx, tx, token.ID)
	})
	return err
}

// tokenSHA256 hashes the raw token the caller presented, the form the
// auth_tokens table stores.
func tokenSHA256(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
