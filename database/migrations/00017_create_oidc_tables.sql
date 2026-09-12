-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Table: public.oidc_clients
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.oidc_clients (
    id TEXT NOT NULL PRIMARY KEY,
    name TEXT,
    secret TEXT,
    callback_urls JSONB,
    logout_callback_urls JSONB,
    image_type VARCHAR(10),
    dark_image_type TEXT,
    launch_url TEXT,
    credentials JSONB,
    is_public BOOLEAN DEFAULT FALSE,
    pkce_enabled BOOLEAN DEFAULT FALSE CHECK (pkce_enabled IN (TRUE, FALSE)),
    requires_reauthentication BOOLEAN NOT NULL DEFAULT FALSE,
    is_group_restricted BOOLEAN NOT NULL DEFAULT FALSE,
    created_by_id UUID,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (created_by_id) REFERENCES public.users(id) ON DELETE SET NULL
) USING heap;

-- --------------------------------------------------------
-- Table: public.custom_claims
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.custom_claims (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    key TEXT NOT NULL,
    value TEXT NOT NULL,
    user_id UUID,
    user_group_id UUID,
    UNIQUE (key, user_id, user_group_id),
    CHECK (user_id IS NOT NULL OR user_group_id IS NOT NULL),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    FOREIGN KEY (user_group_id) REFERENCES user_groups(id) ON DELETE CASCADE
) USING heap;

CREATE INDEX IF NOT EXISTS idx_custom_claims_user_id ON public.custom_claims USING btree (user_id);
CREATE INDEX IF NOT EXISTS idx_custom_claims_user_group_id ON public.custom_claims USING btree (user_group_id);

-- --------------------------------------------------------
-- Table: public.oidc_authorization_codes
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.oidc_authorization_codes (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    code TEXT NOT NULL UNIQUE,
    scope TEXT NOT NULL,
    nonce TEXT,
    code_challenge TEXT,
    code_challenge_method_sha256 BOOLEAN,
    authentication_method TEXT NOT NULL DEFAULT '',
    user_id UUID NOT NULL,
    client_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    FOREIGN KEY (client_id) REFERENCES oidc_clients(id) ON DELETE CASCADE
) USING heap;

CREATE INDEX IF NOT EXISTS idx_oidc_authorization_codes_expires_at ON public.oidc_authorization_codes USING btree (expires_at);

-- --------------------------------------------------------
-- Table: public.user_authorized_oidc_clients (junction table)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.user_authorized_oidc_clients (
    user_id UUID NOT NULL,
    client_id TEXT NOT NULL,
    scope TEXT,
    last_used_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (user_id, client_id),
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    FOREIGN KEY (client_id) REFERENCES oidc_clients(id) ON DELETE CASCADE
) USING heap;

CREATE INDEX IF NOT EXISTS idx_user_authorized_oidc_clients_last_used_at ON public.user_authorized_oidc_clients USING btree (last_used_at);

-- --------------------------------------------------------
-- Table: public.oidc_clients_allowed_user_groups (junction table)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.oidc_clients_allowed_user_groups (
    user_group_id UUID NOT NULL,
    oidc_client_id TEXT NOT NULL,
    PRIMARY KEY (oidc_client_id, user_group_id),
    FOREIGN KEY (user_group_id) REFERENCES user_groups(id) ON DELETE CASCADE,
    FOREIGN KEY (oidc_client_id) REFERENCES oidc_clients(id) ON DELETE CASCADE
) USING heap;

-- --------------------------------------------------------
-- Table: public.oidc_refresh_tokens
-- --------------------------------------------------------

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
    FOREIGN KEY (client_id) REFERENCES oidc_clients(id) ON DELETE CASCADE
) USING heap;

CREATE INDEX IF NOT EXISTS idx_oidc_refresh_tokens_expires_at ON public.oidc_refresh_tokens USING btree (expires_at);

-- --------------------------------------------------------
-- Table: public.oidc_device_codes
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.oidc_device_codes (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    device_code TEXT NOT NULL UNIQUE,
    user_code TEXT NOT NULL UNIQUE,
    scope TEXT NOT NULL,
    is_authorized BOOLEAN NOT NULL DEFAULT FALSE,
    nonce TEXT,
    authentication_method TEXT NOT NULL DEFAULT '',
    user_id UUID,
    client_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL,
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    FOREIGN KEY (client_id) REFERENCES oidc_clients(id) ON DELETE CASCADE
) USING heap;

CREATE INDEX IF NOT EXISTS idx_oidc_device_codes_expires_at ON public.oidc_device_codes USING btree (expires_at);

-- --------------------------------------------------------
-- Table: public.scim_service_providers
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.scim_service_providers (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    endpoint TEXT NOT NULL,
    token TEXT NOT NULL,
    oidc_client_id TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_synced_at TIMESTAMPTZ,
    FOREIGN KEY (oidc_client_id) REFERENCES oidc_clients(id) ON DELETE CASCADE
) USING heap;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_oidc_device_codes_expires_at;
DROP INDEX IF EXISTS idx_oidc_refresh_tokens_expires_at;
DROP INDEX IF EXISTS idx_oidc_authorization_codes_expires_at;
DROP INDEX IF EXISTS idx_user_authorized_oidc_clients_last_used_at;
DROP INDEX IF EXISTS idx_custom_claims_user_group_id;
DROP INDEX IF EXISTS idx_custom_claims_user_id;

-- Drop tables in reverse order of creation (tables with FKs first)
DROP TABLE IF EXISTS public.scim_service_providers;
DROP TABLE IF EXISTS public.oidc_device_codes;
DROP TABLE IF EXISTS public.oidc_refresh_tokens;
DROP TABLE IF EXISTS public.oidc_clients_allowed_user_groups;
DROP TABLE IF EXISTS public.user_authorized_oidc_clients;
DROP TABLE IF EXISTS public.oidc_authorization_codes;
DROP TABLE IF EXISTS public.custom_claims;
DROP TABLE IF EXISTS public.oidc_clients;

-- +goose StatementEnd
