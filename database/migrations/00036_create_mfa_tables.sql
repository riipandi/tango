-- +goose Up
-- +goose StatementBegin

-- Dedicated TOTP MFA state: one enroll row per user (secret sealed
-- with the canonical enc: prefix), hashed single-use recovery codes,
-- and the short-lived pending-auth bridge between a successful
-- password sign-in and full session issuance.

CREATE TABLE IF NOT EXISTS public.user_mfa_totp (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL UNIQUE REFERENCES public.users (id) ON DELETE CASCADE,
    secret_enc TEXT NOT NULL CHECK (secret_enc LIKE 'enc:%'),
    digits SMALLINT NOT NULL DEFAULT 6 CHECK (digits IN (6, 8)),
    period SMALLINT NOT NULL DEFAULT 30 CHECK (period BETWEEN 15 AND 120),
    algorithm TEXT NOT NULL DEFAULT 'SHA1' CHECK (algorithm IN ('SHA1', 'SHA256', 'SHA512')),
    confirmed_at TIMESTAMPTZ,
    last_used_step BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_user_mfa_totp_user_id ON public.user_mfa_totp (user_id);

CREATE TABLE IF NOT EXISTS public.user_mfa_recovery_codes (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL REFERENCES public.users (id) ON DELETE CASCADE,
    code_hash TEXT NOT NULL UNIQUE,
    used_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_user_mfa_recovery_codes_user_id ON public.user_mfa_recovery_codes (user_id);
CREATE INDEX IF NOT EXISTS idx_user_mfa_recovery_codes_user_used
    ON public.user_mfa_recovery_codes (user_id, used_at);

CREATE TABLE IF NOT EXISTS public.user_mfa_pending (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL UNIQUE REFERENCES public.users (id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > CURRENT_TIMESTAMP),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_user_mfa_pending_expires_at
    ON public.user_mfa_pending (expires_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS public.user_mfa_pending;
DROP TABLE IF EXISTS public.user_mfa_recovery_codes;
DROP TABLE IF EXISTS public.user_mfa_totp;

-- +goose StatementEnd
