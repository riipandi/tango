-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Table: public.oauth_connections (for OAuth connections)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.oauth_connections (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL, -- OAuth provider name: 'google', 'github', 'custom-oauth2', etc.
    provider_account_id TEXT NOT NULL, -- User's unique ID on the provider's system
    access_token TEXT, -- OAuth access token for making API calls to provider
    refresh_token TEXT, -- OAuth refresh token for renewing access
    provider_data JSONB, -- OAuth provider's user profile
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > CURRENT_TIMESTAMP),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL,
    UNIQUE(provider, provider_account_id)
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_oauth_connections_updated_at BEFORE UPDATE ON public.oauth_connections FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop triggers in reverse order
DROP TRIGGER IF EXISTS trg_oauth_connections_updated_at ON public.oauth_connections;

-- Drop indexes in reverse order of creation

-- Drop tables in reverse order of creation (tables with FKs first)
DROP TABLE IF EXISTS public.oauth_connections;

-- +goose StatementEnd
