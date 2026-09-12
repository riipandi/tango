-- +goose Up
-- +goose StatementBegin

-- ============================================================================
-- Create Webhook tables
-- ============================================================================

CREATE TABLE IF NOT EXISTS public.webhook_events (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    name TEXT NOT NULL, -- Example: user.login-xxxxxxxxxx
    description TEXT,
    endpoint TEXT,
    method TEXT NOT NULL CHECK (method IN ('GET', 'POST', 'PUT', 'PATCH', 'DELETE')),
    headers JSONB DEFAULT NULL, -- Example: {"k1":"v1","k2":"v2"}
    enabled BOOLEAN NOT NULL DEFAULT TRUE CHECK (enabled IN (TRUE, FALSE)),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL,
    CONSTRAINT webhook_events_name_key UNIQUE (name),
    -- Webhook name only allows alphanumeric characters, dash, and underscores (3-100 chars)
    CONSTRAINT chk_webhook_name_format CHECK (name IS NULL OR name ~ '^[a-zA-Z0-9_-]{3,100}$')
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_webhook_events_updated_at BEFORE UPDATE ON public.webhook_events FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

CREATE TABLE IF NOT EXISTS public.webhook_logs (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    webhook_id UUID REFERENCES public.webhook_events(id) ON DELETE SET NULL,
    http_status INTEGER,
    response JSONB DEFAULT NULL,
    request JSONB DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_webhook_logs_updated_at BEFORE UPDATE ON public.webhook_logs FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- ============================================================================
-- Webhook Events Indexes
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_webhook_events_name ON public.webhook_events (name);
CREATE INDEX IF NOT EXISTS idx_webhook_events_enabled ON public.webhook_events (enabled);
CREATE INDEX IF NOT EXISTS idx_webhook_events_created_at ON public.webhook_events (created_at);
CREATE INDEX IF NOT EXISTS idx_webhook_events_updated_at ON public.webhook_events (updated_at) WHERE updated_at IS NOT NULL;
-- GIN index for headers JSONB to speed up key/value queries
CREATE INDEX IF NOT EXISTS idx_webhook_events_headers_gin ON public.webhook_events USING GIN (headers);

-- ============================================================================
-- Webhook Logs Indexes
-- ============================================================================

CREATE INDEX IF NOT EXISTS idx_webhook_logs_webhook_id ON public.webhook_logs (webhook_id);
CREATE INDEX IF NOT EXISTS idx_webhook_logs_http_status ON public.webhook_logs (http_status);
CREATE INDEX IF NOT EXISTS idx_webhook_logs_created_at ON public.webhook_logs (created_at);
CREATE INDEX IF NOT EXISTS idx_webhook_logs_updated_at ON public.webhook_logs (updated_at) WHERE updated_at IS NOT NULL;
-- GIN indexes for JSONB columns to speed up containment queries
CREATE INDEX IF NOT EXISTS idx_webhook_logs_response_gin ON public.webhook_logs USING GIN (response);
CREATE INDEX IF NOT EXISTS idx_webhook_logs_request_gin ON public.webhook_logs USING GIN (request);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop updated_at triggers
DROP TRIGGER IF EXISTS trg_webhook_logs_updated_at ON public.webhook_logs;
DROP TRIGGER IF EXISTS trg_webhook_events_updated_at ON public.webhook_events;

-- Drop webhook_logs indexes
DROP INDEX IF EXISTS idx_webhook_logs_request_gin;
DROP INDEX IF EXISTS idx_webhook_logs_response_gin;
DROP INDEX IF EXISTS idx_webhook_logs_updated_at;
DROP INDEX IF EXISTS idx_webhook_logs_created_at;
DROP INDEX IF EXISTS idx_webhook_logs_http_status;
DROP INDEX IF EXISTS idx_webhook_logs_webhook_id;

-- Drop webhook_events indexes
DROP INDEX IF EXISTS idx_webhook_events_headers_gin;
DROP INDEX IF EXISTS idx_webhook_events_updated_at;
DROP INDEX IF EXISTS idx_webhook_events_created_at;
DROP INDEX IF EXISTS idx_webhook_events_enabled;
DROP INDEX IF EXISTS idx_webhook_events_name;

-- Drop tables
DROP TABLE IF EXISTS public.webhook_logs;
DROP TABLE IF EXISTS public.webhook_events;

-- +goose StatementEnd
