-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Table: public.sessions
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.sessions (
    id TEXT NOT NULL PRIMARY KEY UNIQUE,
    user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    user_agent TEXT,
    device_name TEXT,
    device_fingerprint TEXT,
    ip_address INET,
    totp_pending BOOLEAN NOT NULL DEFAULT FALSE,
    oauth_groups TEXT NULL,
    oauth_name TEXT NULL,
    oauth_sub TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > CURRENT_TIMESTAMP),
    refreshed_at TIMESTAMPTZ DEFAULT NULL,
    revoked_at TIMESTAMPTZ DEFAULT NULL,
    revoked_by UUID REFERENCES public.users(id) ON DELETE SET NULL,
    impersonated_by UUID REFERENCES public.users(id) ON DELETE SET NULL
) USING heap;

-- Indexes for `public.sessions` table
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON public.sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON public.sessions (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_user_id_expires_at ON public.sessions USING btree (user_id, expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_ip_address ON public.sessions (ip_address) WHERE ip_address IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_device_fingerprint ON public.sessions (device_fingerprint);
CREATE INDEX IF NOT EXISTS idx_sessions_impersonated_by ON public.sessions (impersonated_by) WHERE impersonated_by IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_oauth_sub ON public.sessions USING btree (oauth_sub);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_token_hash ON public.sessions (token_hash);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_sessions_token_hash;
DROP INDEX IF EXISTS idx_sessions_oauth_sub;
DROP INDEX IF EXISTS idx_sessions_impersonated_by;
DROP INDEX IF EXISTS idx_sessions_device_fingerprint;
DROP INDEX IF EXISTS idx_sessions_ip_address;
DROP INDEX IF EXISTS idx_sessions_user_id_expires_at;
DROP INDEX IF EXISTS idx_sessions_expires_at;
DROP INDEX IF EXISTS idx_sessions_user_id;

-- Drop the table
DROP TABLE IF EXISTS public.sessions;

-- +goose StatementEnd
