-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Table: public.queue_tasks
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.queue_tasks (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    queue TEXT NOT NULL,
    task BYTEA NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    wait_until TIMESTAMPTZ,
    claimed_at TIMESTAMPTZ,
    last_executed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (attempts >= 0)
);

CREATE INDEX IF NOT EXISTS idx_queue_tasks_fetch ON public.queue_tasks (wait_until ASC, id ASC) WHERE wait_until IS NOT NULL;

-- --------------------------------------------------------
-- Table: public.queue_tasks_completed
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.queue_tasks_completed (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    queue TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    last_duration_micro BIGINT NOT NULL,
    succeeded BOOLEAN NOT NULL DEFAULT FALSE,
    task BYTEA,
    error TEXT,
    expires_at TIMESTAMPTZ,
    last_executed_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (attempts >= 0)
);

CREATE INDEX IF NOT EXISTS idx_queue_tasks_completed_expires ON public.queue_tasks_completed (expires_at) WHERE expires_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_queue_tasks_completed_expires;
DROP INDEX IF EXISTS idx_queue_tasks_fetch;

DROP TABLE IF EXISTS public.queue_tasks_completed;
DROP TABLE IF EXISTS public.queue_tasks;

-- +goose StatementEnd
