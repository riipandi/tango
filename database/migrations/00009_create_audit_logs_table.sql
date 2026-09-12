-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Table: public.audit_logs
-- Track user actions and system events
-- --------------------------------------------------------

DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'audit_action_status') THEN
    CREATE TYPE public.audit_action_status AS ENUM ('success', 'failed', 'pending', 'unknown');
END IF; END$$;

DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'audit_event_trigger') THEN
    CREATE TYPE public.audit_event_trigger AS ENUM ('user', 'system', 'external');
END IF; END$$;

CREATE TABLE IF NOT EXISTS public.audit_logs (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    event TEXT NOT NULL,
    trigger_type public.audit_event_trigger NOT NULL,
    action_status public.audit_action_status DEFAULT 'pending',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    ip_address INET,
    user_agent TEXT,
    country TEXT,
    city TEXT,
    resource_type TEXT,
    resource_id UUID,
    user_id UUID REFERENCES public.users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
) USING heap;

CREATE INDEX IF NOT EXISTS idx_audit_logs_client_name ON public.audit_logs USING btree (((payload ->> 'client_name'::text)));
CREATE INDEX IF NOT EXISTS idx_audit_logs_country ON public.audit_logs USING btree (country);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON public.audit_logs USING btree (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_event ON public.audit_logs USING btree (event);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_agent ON public.audit_logs USING btree (user_agent);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON public.audit_logs USING btree (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_audit_logs_user_id;
DROP INDEX IF EXISTS idx_audit_logs_user_agent;
DROP INDEX IF EXISTS idx_audit_logs_event;
DROP INDEX IF EXISTS idx_audit_logs_created_at;
DROP INDEX IF EXISTS idx_audit_logs_country;
DROP INDEX IF EXISTS idx_audit_logs_client_name;

-- Drop the table itself
DROP TABLE IF EXISTS public.audit_logs;

-- Drop the custom enum type
DROP TYPE IF EXISTS public.audit_event_trigger;
DROP TYPE IF EXISTS public.audit_action_status;

-- +goose StatementEnd
