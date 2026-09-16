package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/logger"
)

// Retention settings for recurring cleanup jobs.
const (
	// TokenCleanupInterval is how often expired tokens are swept.
	TokenCleanupInterval = 6 * time.Hour
	// TokenGrace keeps expired rows briefly for clock-skewed verification.
	TokenGrace = time.Hour

	// WebhookLogCleanupInterval controls delivery log cleanup.
	WebhookLogCleanupInterval = 12 * time.Hour
	// WebhookLogRetention bounds the delivery history.
	WebhookLogRetention = 30 * 24 * time.Hour
)

// LogPruner deletes delivery records before a cutoff.
type LogPruner interface {
	PruneDeliveries(ctx context.Context, before time.Time) (int64, error)
}

// CleanupTokens builds the recurring job that removes expired tokens and sessions.
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

// CleanupWebhookDeliveries builds the recurring delivery cleanup job.
func CleanupWebhookDeliveries(pruner LogPruner, log logger.Logger) Job {
	return Job{
		Name:     "cleanup_webhook_deliveries",
		Interval: WebhookLogCleanupInterval,
		Run: func(ctx context.Context) error {
			cutoff := now().Add(-WebhookLogRetention)
			removed, err := pruner.PruneDeliveries(ctx, cutoff)
			if err != nil {
				return err
			}
			if removed > 0 {
				log.Info(fmt.Sprintf("jobs: webhook delivery cleanup removed %d rows", removed))
			}
			return nil
		},
	}
}
