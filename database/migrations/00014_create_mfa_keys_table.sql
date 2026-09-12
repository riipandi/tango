-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Table: public.mfa_keys
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.mfa_keys (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL REFERENCES public.users (id) ON DELETE CASCADE,
    device_name TEXT NOT NULL,
    device_type TEXT NOT NULL CHECK (device_type IN ('authenticator', 'email', 'sms', 'whatsapp')),
    secret_key TEXT NOT NULL CHECK (LENGTH(secret_key) >= 16), -- encrypted (AES256)
    backup_codes JSONB NOT NULL DEFAULT '[]'::jsonb, -- hashed backup codes
    verified_at TIMESTAMPTZ DEFAULT NULL,
    last_used_at TIMESTAMPTZ DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_mfa_keys_updated_at BEFORE UPDATE ON public.mfa_keys FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- Indexes for `public.mfa_keys` table (GIN index on backup_codes used to speed up containment queries)
CREATE INDEX IF NOT EXISTS idx_mfa_keys_backup_codes_gin ON public.mfa_keys USING GIN (backup_codes);
CREATE INDEX IF NOT EXISTS idx_mfa_keys_user_id ON public.mfa_keys (user_id);
CREATE INDEX IF NOT EXISTS idx_mfa_keys_updated_at ON public.mfa_keys (updated_at) WHERE updated_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_mfa_keys_last_used_at ON public.mfa_keys (last_used_at) WHERE last_used_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_mfa_keys_verified_at ON public.mfa_keys (verified_at) WHERE verified_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop triggers in reverse order
DROP TRIGGER IF EXISTS trg_mfa_keys_updated_at ON public.mfa_keys;

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_mfa_keys_verified_at;
DROP INDEX IF EXISTS idx_mfa_keys_last_used_at;
DROP INDEX IF EXISTS idx_mfa_keys_updated_at;
DROP INDEX IF EXISTS idx_mfa_keys_user_id;
DROP INDEX IF EXISTS idx_mfa_keys_backup_codes_gin;

-- Drop the table
DROP TABLE IF EXISTS public.mfa_keys;

-- +goose StatementEnd
