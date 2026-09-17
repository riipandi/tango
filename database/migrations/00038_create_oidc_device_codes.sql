-- +goose Up
-- +goose StatementBegin

-- ============================================================================
-- OAuth 2.0 Device Flow (RFC 8628): one row per device authorization.
-- Device and user codes are stored as SHA-256 hashes; status drives the
-- token poll (pending → approved/denied → consumed).
-- ============================================================================

CREATE TABLE public.oidc_device_codes (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    device_code_hash TEXT NOT NULL UNIQUE,
    user_code_hash TEXT NOT NULL UNIQUE,
    scope TEXT NOT NULL,
    resource TEXT,
    nonce TEXT,
    client_id TEXT NOT NULL,
    user_id UUID,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'denied', 'consumed')),
    last_polled_at TIMESTAMPTZ DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL,
    approved_at TIMESTAMPTZ DEFAULT NULL,
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    FOREIGN KEY (client_id) REFERENCES public.oidc_clients(id) ON DELETE CASCADE,
    CONSTRAINT chk_oidc_device_codes_expiry CHECK (expires_at > created_at)
) USING heap;

CREATE INDEX idx_oidc_device_codes_expires_at ON public.oidc_device_codes (expires_at);
CREATE INDEX idx_oidc_device_codes_status ON public.oidc_device_codes (status);

-- PAR (RFC 9126): pushed authorization requests live in
-- oauth2_sessions with kind 'par'; the CHECK on client_id applies.

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP INDEX IF EXISTS idx_oidc_device_codes_status;
DROP INDEX IF EXISTS idx_oidc_device_codes_expires_at;
DROP TABLE IF EXISTS public.oidc_device_codes;

-- +goose StatementEnd
