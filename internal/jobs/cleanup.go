package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/logger"
)

// Retention windows for the recurring cleanups. Transactions that
// already expired never resolve again, so the expired rows are dead
// weight in the token tables.
const (
	// TokenCleanupInterval is how often expired tokens are swept.
	TokenCleanupInterval = 6 * time.Hour
	// TokenGrace keeps expired rows briefly so a clock-skewed verify
	// can still distinguish "expired" from "unknown".
	TokenGrace = time.Hour

	// WebhookLogCleanupInterval matches the queue's own retention for
	// completed delivery tasks, so both views age out together.
	WebhookLogCleanupInterval = 12 * time.Hour
	// WebhookLogRetention bounds the delivery history.
	WebhookLogRetention = 30 * 24 * time.Hour
)

// LogPruner deletes delivery logs recorded before the cutoff;
// implemented by the webhook store.
type LogPruner interface {
	PruneLogs(ctx context.Context, before time.Time) (int64, error)
}

// CleanupTokens builds the recurring job that sweeps expired
// single-use tokens and sessions: auth_tokens (one-time access, email
// verification, reauthentication), signup_tokens, and expired or
// revoked sessions. Rows are deleted in one transaction so a partial
// sweep cannot leave a session without its token.
func CleanupTokens(db datastore.Store, log logger.Logger) Job {
	return Job{
		Name:     "cleanup_tokens",
		Interval: TokenCleanupInterval,
		Run: func(ctx context.Context) error {
			cutoff := now().Add(-TokenGrace)

			var deleted int64
			err := db.WithTx(ctx, func(exec datastore.Executor) error {
				for _, statement := range []struct {
					query string
					args  []any
				}{
					{"DELETE FROM public.auth_tokens WHERE expires_at < $1", []any{cutoff}},
					{"DELETE FROM public.signup_tokens WHERE expires_at < $1", []any{cutoff}},
					{"DELETE FROM public.sessions WHERE expires_at < $1 OR revoked_at IS NOT NULL", []any{cutoff}},
					{"DELETE FROM public.device_login_requests WHERE expires_at < $1", []any{cutoff}},
				} {
					tag, execErr := exec.Exec(ctx, statement.query, statement.args...)
					if execErr != nil {
						return fmt.Errorf("jobs: %s: %w", statement.query, execErr)
					}
					deleted += tag.RowsAffected()
				}
				return nil
			})
			if err != nil {
				return err
			}

			if deleted > 0 {
				log.Info(fmt.Sprintf("jobs: token cleanup removed %d rows", deleted))
			}
			return nil
		},
	}
}

// CleanupWebhookLogs builds the recurring job that prunes delivery
// history past the retention window.
func CleanupWebhookLogs(pruner LogPruner, log logger.Logger) Job {
	return Job{
		Name:     "cleanup_webhook_logs",
		Interval: WebhookLogCleanupInterval,
		Run: func(ctx context.Context) error {
			cutoff := now().Add(-WebhookLogRetention)
			removed, err := pruner.PruneLogs(ctx, cutoff)
			if err != nil {
				return err
			}
			if removed > 0 {
				log.Info(fmt.Sprintf("jobs: webhook log cleanup removed %d rows", removed))
			}
			return nil
		},
	}
}
