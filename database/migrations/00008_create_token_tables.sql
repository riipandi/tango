-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Table: public.auth_tokens
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.auth_tokens (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    device_token VARCHAR(16), -- Used only for one_time_access
    purpose TEXT NOT NULL DEFAULT 'one_time_access',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > CURRENT_TIMESTAMP),
    last_sent_at TIMESTAMPTZ DEFAULT NULL,
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    CONSTRAINT chk_auth_token_purpose CHECK (purpose IN ('email_verification', 'one_time_access', 'reauthentication'))
) USING heap;

-- Indexes for `public.auth_tokens` table
CREATE INDEX IF NOT EXISTS idx_auth_tokens_user_id ON public.auth_tokens (user_id);
CREATE INDEX IF NOT EXISTS idx_auth_tokens_purpose ON public.auth_tokens (purpose);
CREATE INDEX IF NOT EXISTS idx_auth_tokens_expires_at ON public.auth_tokens USING btree (expires_at);
CREATE INDEX IF NOT EXISTS idx_auth_tokens_token_hash ON public.auth_tokens USING HASH (token_hash);
CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_tokens_user_id_purpose ON public.auth_tokens USING btree (user_id, purpose);

-- --------------------------------------------------------
-- Table: public.signup_tokens
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.signup_tokens (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    token_hash TEXT NOT NULL UNIQUE,
    usage_limit INTEGER NOT NULL DEFAULT 1,
    usage_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL
) USING heap;

-- Indexes for `public.signup_tokens` table
CREATE INDEX IF NOT EXISTS idx_signup_tokens_expires_at ON public.signup_tokens USING btree (expires_at);

-- --------------------------------------------------------
-- Table: public.signup_tokens_user_groups (junction table)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.signup_tokens_user_groups (
    signup_token_id UUID NOT NULL,
    user_group_id UUID NOT NULL,
    PRIMARY KEY (signup_token_id, user_group_id),
    FOREIGN KEY (signup_token_id) REFERENCES signup_tokens(id) ON DELETE CASCADE,
    FOREIGN KEY (user_group_id) REFERENCES user_groups(id) ON DELETE CASCADE
) USING heap;

-- --------------------------------------------------------
-- Table: public.refresh_tokens
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.refresh_tokens (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL REFERENCES public.sessions(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    ip_address INET,
    user_agent TEXT,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > CURRENT_TIMESTAMP),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at TIMESTAMPTZ DEFAULT NULL,
    revoked_by UUID REFERENCES public.users(id) ON DELETE SET NULL
) USING heap;

-- Indexes for `public.refresh_tokens` table
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON public.refresh_tokens (user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_session_id ON public.refresh_tokens (session_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires_at ON public.refresh_tokens (expires_at);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_revoked_at ON public.refresh_tokens (revoked_at) WHERE revoked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_created_at ON public.refresh_tokens (created_at);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_ip_address ON public.refresh_tokens (ip_address) WHERE ip_address IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_agent ON public.refresh_tokens (user_agent) WHERE user_agent IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_refresh_tokens_token_hash ON public.refresh_tokens (token_hash);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_refresh_tokens_token_hash;
DROP INDEX IF EXISTS idx_refresh_tokens_user_agent;
DROP INDEX IF EXISTS idx_refresh_tokens_ip_address;
DROP INDEX IF EXISTS idx_refresh_tokens_created_at;
DROP INDEX IF EXISTS idx_refresh_tokens_revoked_at;
DROP INDEX IF EXISTS idx_refresh_tokens_expires_at;
DROP INDEX IF EXISTS idx_refresh_tokens_session_id;
DROP INDEX IF EXISTS idx_refresh_tokens_user_id;
DROP INDEX IF EXISTS idx_signup_tokens_expires_at;
DROP INDEX IF EXISTS idx_auth_tokens_user_id_purpose;
DROP INDEX IF EXISTS idx_auth_tokens_token_hash;
DROP INDEX IF EXISTS idx_auth_tokens_expires_at;
DROP INDEX IF EXISTS idx_auth_tokens_purpose;
DROP INDEX IF EXISTS idx_auth_tokens_user_id;

-- Drop tables in reverse order of creation (tables with FKs first)
DROP TABLE IF EXISTS public.refresh_tokens;
DROP TABLE IF EXISTS public.signup_tokens_user_groups;
DROP TABLE IF EXISTS public.signup_tokens;
DROP TABLE IF EXISTS public.auth_tokens;

-- +goose StatementEnd
