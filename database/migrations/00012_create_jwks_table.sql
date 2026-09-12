-- +goose Up
-- +goose StatementBegin

-- This table stores JSON Web Key Sets (JWKS) for signing and verifying JWTs
-- This table is optional, depending on whether you want to manage your own keys
-- or use a third-party service. Example use case: multi-tenant applications.

-- --------------------------------------------------------
-- Table: public.jwks
-- --------------------------------------------------------

DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'jwt_algorithm') THEN
    CREATE TYPE public.jwt_algorithm AS ENUM (
        'HS256',
        'HS384',
        'HS512',
        'RS256',
        'RS384',
        'RS512',
        'ES256',
        'ES384',
        'ES512'
    );
END IF; END$$;

CREATE TABLE IF NOT EXISTS public.jwks (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    key_id TEXT NOT NULL UNIQUE, -- kid
    algorithm public.jwt_algorithm NOT NULL DEFAULT 'HS256',
    key_type TEXT NOT NULL, -- e.g. RSA, EC, oct
    public_key BYTEA NOT NULL, -- public key for verification (encrypted)
    private_key BYTEA, -- nullable, only for internal use (encrypted)
    use_for TEXT NOT NULL DEFAULT 'sig', -- 'sig' (signature) or 'enc' (encryption)
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    expires_at TIMESTAMPTZ DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_jwks_updated_at BEFORE UPDATE ON public.jwks FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- Indexes for `public.jwks` table
CREATE INDEX IF NOT EXISTS idx_jwks_active ON public.jwks(is_active);
CREATE INDEX IF NOT EXISTS idx_jwks_algorithm ON public.jwks(algorithm);
CREATE INDEX IF NOT EXISTS idx_jwks_use_for ON public.jwks(use_for);
CREATE INDEX IF NOT EXISTS idx_jwks_expires_at ON public.jwks(expires_at);
CREATE INDEX IF NOT EXISTS idx_jwks_active_algorithm_use_for ON public.jwks(is_active, algorithm, use_for);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop the trigger first
DROP TRIGGER IF EXISTS trg_jwks_updated_at ON public.jwks;

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_jwks_active_algorithm_use_for;
DROP INDEX IF EXISTS idx_jwks_expires_at;
DROP INDEX IF EXISTS idx_jwks_use_for;
DROP INDEX IF EXISTS idx_jwks_algorithm;
DROP INDEX IF EXISTS idx_jwks_active;

-- Drop the table
DROP TABLE IF EXISTS public.jwks;

-- Drop the custom enum type
DROP TYPE IF EXISTS public.jwt_algorithm;

-- +goose StatementEnd
