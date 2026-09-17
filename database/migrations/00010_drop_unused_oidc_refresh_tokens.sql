-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Drop public.oidc_refresh_tokens: runtime refresh tokens are
-- Fosite-style oauth2_sessions rows (kind=refresh); this table has
-- no caller.
-- --------------------------------------------------------

DROP TABLE IF EXISTS public.oidc_refresh_tokens;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

CREATE TABLE IF NOT EXISTS public.oidc_refresh_tokens (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    token TEXT NOT NULL UNIQUE,
    scope TEXT NOT NULL,
    authentication_method TEXT NOT NULL DEFAULT '',
    user_id UUID NOT NULL,
    client_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    FOREIGN KEY (client_id) REFERENCES public.oidc_clients(id) ON DELETE CASCADE
) USING heap;

CREATE INDEX IF NOT EXISTS idx_oidc_refresh_tokens_expires_at ON public.oidc_refresh_tokens USING btree (expires_at);

-- +goose StatementEnd
