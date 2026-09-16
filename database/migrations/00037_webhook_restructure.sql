-- +goose Up
-- +goose StatementBegin

-- ============================================================================
-- Webhook restructure: webhook_events -> webhook_endpoints, webhook_logs ->
-- webhook_deliveries (+ webhook_delivery_attempts) with immutable body bytes.
-- ============================================================================

ALTER TABLE public.webhook_events RENAME TO webhook_endpoints;

ALTER INDEX IF EXISTS idx_webhook_events_name RENAME TO idx_webhook_endpoints_name;
ALTER INDEX IF EXISTS idx_webhook_events_enabled RENAME TO idx_webhook_endpoints_enabled;
ALTER INDEX IF EXISTS idx_webhook_events_created_at RENAME TO idx_webhook_endpoints_created_at;
ALTER INDEX IF EXISTS idx_webhook_events_updated_at RENAME TO idx_webhook_endpoints_updated_at;
ALTER INDEX IF EXISTS idx_webhook_events_headers_gin RENAME TO idx_webhook_endpoints_headers_gin;
ALTER INDEX IF EXISTS idx_webhook_events_event_types RENAME TO idx_webhook_endpoints_event_types;
ALTER INDEX IF EXISTS webhook_events_name_key RENAME TO webhook_endpoints_name_key;
ALTER INDEX IF EXISTS webhook_events_pkey RENAME TO webhook_endpoints_pkey;

ALTER TRIGGER trg_webhook_events_updated_at ON public.webhook_endpoints RENAME TO trg_webhook_endpoints_updated_at;

ALTER TABLE public.webhook_endpoints RENAME COLUMN secret TO secret_enc;

CREATE TABLE public.webhook_deliveries (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    webhook_id UUID REFERENCES public.webhook_endpoints(id) ON DELETE SET NULL,
    event TEXT NOT NULL,
    body BYTEA NOT NULL, -- the exact canonical bytes; retries sign and deliver these
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'succeeded', 'failed')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    delivered_at TIMESTAMPTZ DEFAULT NULL
) USING heap;

CREATE TABLE public.webhook_delivery_attempts (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    delivery_id UUID NOT NULL REFERENCES public.webhook_deliveries(id) ON DELETE CASCADE,
    attempt_number INTEGER NOT NULL CHECK (attempt_number > 0),
    response_status INTEGER DEFAULT NULL,
    error TEXT DEFAULT NULL,
    duration_ms INTEGER DEFAULT NULL CHECK (duration_ms IS NULL OR duration_ms >= 0),
    response JSONB DEFAULT NULL, -- redacted response metadata only
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
) USING heap;

-- Carry the old log rows over: only rows with a recorded body and an
-- event name map onto the new shape; aggregates without a body are
-- dropped with the old table.
INSERT INTO public.webhook_deliveries (id, webhook_id, event, body, status, attempt_count, created_at, delivered_at)
SELECT l.id,
       l.webhook_id,
       l.event,
       convert_to(l.request ->> 'body', 'UTF8'),
       CASE WHEN l.succeeded THEN 'succeeded' WHEN l.attempts > 0 THEN 'failed' ELSE 'pending' END,
       l.attempts,
       l.created_at,
       CASE WHEN l.succeeded THEN COALESCE(l.updated_at, l.created_at) ELSE NULL END
FROM public.webhook_logs l
WHERE l.event IS NOT NULL AND l.request ->> 'body' IS NOT NULL;

INSERT INTO public.webhook_delivery_attempts (delivery_id, attempt_number, response_status, error, created_at)
SELECT d.id, d.attempt_count, l.http_status, l.error, COALESCE(l.updated_at, l.created_at)
FROM public.webhook_deliveries d
JOIN public.webhook_logs l ON l.id = d.id
WHERE d.attempt_count > 0;

DROP TABLE public.webhook_logs;

CREATE INDEX idx_webhook_deliveries_webhook_id ON public.webhook_deliveries (webhook_id);
CREATE INDEX idx_webhook_deliveries_event ON public.webhook_deliveries (event);
CREATE INDEX idx_webhook_deliveries_status ON public.webhook_deliveries (status);
CREATE INDEX idx_webhook_deliveries_created_at_desc ON public.webhook_deliveries (created_at DESC);
CREATE INDEX idx_webhook_delivery_attempts_delivery ON public.webhook_delivery_attempts (delivery_id, attempt_number);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE TABLE public.webhook_logs (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    webhook_id UUID REFERENCES public.webhook_endpoints(id) ON DELETE SET NULL,
    http_status INTEGER,
    response JSONB DEFAULT NULL,
    request JSONB DEFAULT NULL,
    -- Columns from 00027 are restored verbatim: this migration runs
    -- after 00027 is already applied, so the recreated table must
    -- carry its shape.
    event TEXT,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    succeeded BOOLEAN NOT NULL DEFAULT FALSE,
    error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL
) USING heap;

-- Carry rows back before dropping the new tables.
INSERT INTO public.webhook_logs (id, webhook_id, http_status, response, request, created_at, updated_at)
SELECT d.id,
       d.webhook_id,
       a.response_status,
       a.response,
       jsonb_build_object('body', convert_from(d.body, 'UTF8')),
       d.created_at,
       COALESCE(a.created_at, d.created_at)
FROM public.webhook_deliveries d
LEFT JOIN public.webhook_delivery_attempts a ON a.delivery_id = d.id AND a.attempt_number = d.attempt_count;

DROP INDEX IF EXISTS idx_webhook_delivery_attempts_delivery;
DROP TABLE IF EXISTS public.webhook_delivery_attempts;

DROP INDEX IF EXISTS idx_webhook_deliveries_created_at_desc;
DROP INDEX IF EXISTS idx_webhook_deliveries_status;
DROP INDEX IF EXISTS idx_webhook_deliveries_event;
DROP INDEX IF EXISTS idx_webhook_deliveries_webhook_id;
DROP TABLE IF EXISTS public.webhook_deliveries;

ALTER TABLE public.webhook_endpoints RENAME COLUMN secret_enc TO secret;

ALTER TRIGGER trg_webhook_endpoints_updated_at ON public.webhook_endpoints RENAME TO trg_webhook_events_updated_at;

ALTER INDEX IF EXISTS idx_webhook_endpoints_name RENAME TO idx_webhook_events_name;
ALTER INDEX IF EXISTS idx_webhook_endpoints_enabled RENAME TO idx_webhook_events_enabled;
ALTER INDEX IF EXISTS idx_webhook_endpoints_created_at RENAME TO idx_webhook_events_created_at;
ALTER INDEX IF EXISTS idx_webhook_endpoints_updated_at RENAME TO idx_webhook_events_updated_at;
ALTER INDEX IF EXISTS idx_webhook_endpoints_headers_gin RENAME TO idx_webhook_events_headers_gin;
ALTER INDEX IF EXISTS idx_webhook_endpoints_event_types RENAME TO idx_webhook_events_event_types;
ALTER INDEX IF EXISTS webhook_endpoints_name_key RENAME TO webhook_events_name_key;
ALTER INDEX IF EXISTS webhook_endpoints_pkey RENAME TO webhook_events_pkey;

ALTER TABLE public.webhook_endpoints RENAME TO webhook_events;

-- +goose StatementEnd
