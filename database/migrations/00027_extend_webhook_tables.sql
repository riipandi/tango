-- +goose Up
-- +goose StatementBegin

-- The phase 7 delivery model adds three things to the upstream tables:
-- a per-endpoint signing secret, the event subscription list, and the
-- per-delivery attempt state (attempt counter, event name, outcome).

ALTER TABLE public.webhook_events
    -- AES-256-GCM ciphertext (crypto.Cipher wire format), never plaintext.
    ADD COLUMN IF NOT EXISTS secret TEXT,
    -- Subscribed event names; empty or '*' means every event.
    ADD COLUMN IF NOT EXISTS event_types TEXT[] NOT NULL DEFAULT '{}';

ALTER TABLE public.webhook_logs
    ADD COLUMN IF NOT EXISTS event TEXT,
    ADD COLUMN IF NOT EXISTS attempts INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS succeeded BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS error TEXT,
    ADD CONSTRAINT chk_webhook_logs_attempts CHECK (attempts >= 0);

CREATE INDEX IF NOT EXISTS idx_webhook_events_event_types
    ON public.webhook_events USING GIN (event_types);
CREATE INDEX IF NOT EXISTS idx_webhook_logs_event ON public.webhook_logs (event);
CREATE INDEX IF NOT EXISTS idx_webhook_logs_succeeded ON public.webhook_logs (succeeded);
CREATE INDEX IF NOT EXISTS idx_webhook_logs_created_at_desc ON public.webhook_logs (created_at DESC);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_webhook_logs_created_at_desc;
DROP INDEX IF EXISTS idx_webhook_logs_succeeded;
DROP INDEX IF EXISTS idx_webhook_logs_event;
DROP INDEX IF EXISTS idx_webhook_events_event_types;

ALTER TABLE public.webhook_logs
    DROP CONSTRAINT IF EXISTS chk_webhook_logs_attempts,
    DROP COLUMN IF EXISTS error,
    DROP COLUMN IF EXISTS succeeded,
    DROP COLUMN IF EXISTS attempts,
    DROP COLUMN IF EXISTS event;

ALTER TABLE public.webhook_events
    DROP COLUMN IF EXISTS event_types,
    DROP COLUMN IF EXISTS secret;

-- +goose StatementEnd
