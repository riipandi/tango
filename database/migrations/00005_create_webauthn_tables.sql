-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Table: public.webauthn_credentials
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.webauthn_credentials (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL REFERENCES public.users (id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    credential_id BYTEA NOT NULL,
    public_key BYTEA NOT NULL,
    device_type TEXT NOT NULL,
    attestation_type TEXT NOT NULL,
    transport JSONB DEFAULT '[]'::jsonb,
    backup_eligible BOOLEAN NOT NULL DEFAULT FALSE,
    backup_state BOOLEAN NOT NULL DEFAULT FALSE,
    aaguid VARCHAR(36) CHECK (
        aaguid IS NULL OR
        aaguid ~* '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
    ), -- AAGUID (Authenticator Attestation GUID)
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL,
    last_used_at TIMESTAMPTZ DEFAULT NULL,
    CONSTRAINT unique_credential_id UNIQUE (credential_id)
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_webauthn_credentials_updated_at BEFORE UPDATE ON public.webauthn_credentials FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- Indexes for `public.webauthn_credentials` table
-- NOTICE: Index for Bytea column is only optimal for the search for exact match, not LIKE or range.
CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_user_id ON public.webauthn_credentials USING btree (user_id);
CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_credential_id ON public.webauthn_credentials (credential_id);
CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_device_type ON public.webauthn_credentials (device_type);
CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_backup_eligible ON public.webauthn_credentials (backup_eligible) WHERE backup_eligible = TRUE;
CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_last_used_at ON public.webauthn_credentials (last_used_at) WHERE last_used_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_created_at ON public.webauthn_credentials (created_at);
CREATE INDEX IF NOT EXISTS idx_webauthn_credentials_user_device_type ON public.webauthn_credentials (user_id, device_type);

-- --------------------------------------------------------
-- Table: public.webauthn_sessions
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.webauthn_sessions (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID REFERENCES public.users(id) ON DELETE CASCADE,
    challenge TEXT NOT NULL UNIQUE,
    challenge_type TEXT NOT NULL CHECK (challenge_type IN ('registration', 'authentication')),
    user_verification TEXT NOT NULL DEFAULT 'preferred' CHECK (user_verification IN ('required', 'preferred', 'discouraged')),
    credential_params JSONB NOT NULL DEFAULT '[]'::JSONB,
    extensions JSONB NOT NULL DEFAULT '{}', -- Authenticator extension data from ceremonies
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > CURRENT_TIMESTAMP)
) USING heap;

-- Indexes for `public.webauthn_sessions` table
CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_user_id ON public.webauthn_sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_challenge ON public.webauthn_sessions (challenge);
CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_expires_at ON public.webauthn_sessions USING btree (expires_at);
CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_type ON public.webauthn_sessions (challenge_type);
CREATE INDEX IF NOT EXISTS idx_webauthn_sessions_user_type ON public.webauthn_sessions (user_id, challenge_type);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop trigger first
DROP TRIGGER IF EXISTS trg_webauthn_credentials_updated_at ON public.webauthn_credentials;

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_webauthn_sessions_user_type;
DROP INDEX IF EXISTS idx_webauthn_sessions_type;
DROP INDEX IF EXISTS idx_webauthn_sessions_expires_at;
DROP INDEX IF EXISTS idx_webauthn_sessions_challenge;
DROP INDEX IF EXISTS idx_webauthn_sessions_user_id;
DROP INDEX IF EXISTS idx_webauthn_credentials_user_device_type;
DROP INDEX IF EXISTS idx_webauthn_credentials_created_at;
DROP INDEX IF EXISTS idx_webauthn_credentials_last_used_at;
DROP INDEX IF EXISTS idx_webauthn_credentials_backup_eligible;
DROP INDEX IF EXISTS idx_webauthn_credentials_device_type;
DROP INDEX IF EXISTS idx_webauthn_credentials_credential_id;
DROP INDEX IF EXISTS idx_webauthn_credentials_user_id;

-- Drop the tables themselves
DROP TABLE IF EXISTS public.webauthn_sessions;
DROP TABLE IF EXISTS public.webauthn_credentials;

-- +goose StatementEnd
