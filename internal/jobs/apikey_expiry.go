package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/huandu/go-sqlbuilder"

	"github.com/riipandi/tango/internal/datastore"
	"github.com/riipandi/tango/internal/logger"
	"github.com/riipandi/tango/internal/mailer"
)

// API key expiry reminder settings.
const (
	// APIKeyExpiryInterval is how often the reminder sweep runs.
	APIKeyExpiryInterval = 12 * time.Hour
	// APIKeyExpiryWindow is how far ahead the sweep looks.
	APIKeyExpiryWindow = 7 * 24 * time.Hour
)

// expiringKey is one reminder target.
type expiringKey struct {
	ID        string
	Name      string
	Email     string
	UserName  string
	ExpiresAt time.Time
}

// RemindExpiringAPIKeys builds the recurring API key reminder job.
func RemindExpiringAPIKeys(db datastore.Store, sender MailEnqueuer, log logger.Logger) Job {
	return Job{
		Name:     "remind_expiring_api_keys",
		Interval: APIKeyExpiryInterval,
		Run: func(ctx context.Context) error {
			if sender == nil {
				return nil
			}

			keys, err := expiringAPIKeys(ctx, db)
			if err != nil {
				return err
			}
			if len(keys) == 0 {
				return nil
			}

			sent := 0
			for _, key := range keys {
				msg := mailer.Message{
					To:       key.Email,
					Subject:  "Your API key expires soon",
					Template: "api-key-expiring-soon",
					Data: map[string]any{
						"Name":       key.UserName,
						"APIKeyName": key.Name,
						// Format the time before it enters the JSON queue.
						"ExpiresAt": key.ExpiresAt.Format("2006-01-02 15:04:05 MST"),
					},
				}
				if enqueueErr := sender.EnqueueEmail(ctx, msg); enqueueErr != nil {
					return fmt.Errorf("jobs: api key reminder: %w", enqueueErr)
				}
				if markErr := markReminderSent(ctx, db, key.ID); markErr != nil {
					return markErr
				}
				sent++
			}

			log.Info(fmt.Sprintf("jobs: queued %d API key expiry reminders", sent))
			return nil
		},
	}
}

// MailEnqueuer queues transactional email.
type MailEnqueuer interface {
	EnqueueEmail(ctx context.Context, msg mailer.Message) error
}

// expiringAPIKeys returns unrevoked keys that need a reminder.
func expiringAPIKeys(ctx context.Context, db datastore.Store) ([]expiringKey, error) {
	reference := now()

	sb := sqlbuilder.PostgreSQL.NewSelectBuilder()
	sb.Select("k.id", "k.name", "u.email", "u.display_name", "k.expires_at")
	sb.From("public.api_keys AS k")
	sb.Join("public.users AS u ON u.id = k.user_id")
	sb.Where(
		"k.revoked_at IS NULL",
		"k.expiration_email_sent_at IS NULL",
		sb.GT("k.expires_at", reference),
		sb.LE("k.expires_at", reference.Add(APIKeyExpiryWindow)),
	)
	sb.OrderBy("k.expires_at ASC")

	query, args := sb.Build()
	rows, err := db.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("jobs: select expiring keys: %w", err)
	}
	defer rows.Close()

	out := []expiringKey{}
	for rows.Next() {
		var key expiringKey
		var displayName *string
		if scanErr := rows.Scan(&key.ID, &key.Name, &key.Email, &displayName, &key.ExpiresAt); scanErr != nil {
			return nil, fmt.Errorf("jobs: scan expiring key: %w", scanErr)
		}
		key.UserName = key.Email
		if displayName != nil && *displayName != "" {
			key.UserName = *displayName
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

// markReminderSent marks a key so later sweeps skip it.
func markReminderSent(ctx context.Context, db datastore.Store, keyID string) error {
	ub := sqlbuilder.PostgreSQL.NewUpdateBuilder()
	ub.Update("public.api_keys")
	ub.Set(ub.Assign("expiration_email_sent_at", now()))
	ub.Where(ub.E("id", keyID), "expiration_email_sent_at IS NULL")

	query, args := ub.Build()
	if _, err := db.Exec(ctx, query, args...); err != nil {
		return fmt.Errorf("jobs: mark reminder sent: %w", err)
	}
	return nil
}
