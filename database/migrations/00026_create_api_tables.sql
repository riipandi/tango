-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Table: public.apis (resource servers, "APIs" in Pocket ID)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.apis (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 50),
    resource TEXT NOT NULL UNIQUE CHECK (char_length(resource) BETWEEN 1 AND 350),
    allow_cimd_clients BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL
) USING heap;

CREATE TRIGGER trg_apis_updated_at BEFORE UPDATE ON public.apis FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

CREATE INDEX IF NOT EXISTS idx_apis_created_at ON public.apis (created_at);
CREATE INDEX IF NOT EXISTS idx_apis_name ON public.apis USING gin (name gin_trgm_ops);

-- --------------------------------------------------------
-- Table: public.api_permissions (scopes per API)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.api_permissions (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    api_id UUID NOT NULL REFERENCES public.apis(id) ON DELETE CASCADE,
    key TEXT NOT NULL CHECK (char_length(key) BETWEEN 1 AND 128),
    name TEXT NOT NULL CHECK (char_length(name) BETWEEN 1 AND 50),
    description TEXT CHECK (char_length(description) <= 200),
    allowed_for_cimd_clients BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL,
    -- A permission key is unique within its API; the wire scope is
    -- "<resource>:<key>".
    CONSTRAINT uq_api_permissions_api_key UNIQUE (api_id, key)
) USING heap;

CREATE TRIGGER trg_api_permissions_updated_at BEFORE UPDATE ON public.api_permissions FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

CREATE INDEX IF NOT EXISTS idx_api_permissions_api_id ON public.api_permissions (api_id);

-- --------------------------------------------------------
-- Table: public.oidc_client_api_grants (client → API grants)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.oidc_client_api_grants (
    api_id UUID NOT NULL REFERENCES public.apis(id) ON DELETE CASCADE,
    client_id TEXT NOT NULL REFERENCES public.oidc_clients(id) ON DELETE CASCADE,
    user_delegated_access BOOLEAN NOT NULL DEFAULT FALSE,
    client_access BOOLEAN NOT NULL DEFAULT FALSE,
    cimd_granted_access BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL,
    PRIMARY KEY (api_id, client_id)
) USING heap;

CREATE TRIGGER trg_oidc_client_api_grants_updated_at BEFORE UPDATE ON public.oidc_client_api_grants FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- --------------------------------------------------------
-- Table: public.oidc_client_api_grant_permissions (granted scopes)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.oidc_client_api_grant_permissions (
    api_id UUID NOT NULL,
    client_id TEXT NOT NULL,
    permission_id UUID NOT NULL REFERENCES public.api_permissions(id) ON DELETE CASCADE,
    subject TEXT NOT NULL CHECK (subject IN ('client', 'user_delegated', 'cimd')),
    PRIMARY KEY (api_id, client_id, permission_id, subject),
    FOREIGN KEY (api_id, client_id) REFERENCES public.oidc_client_api_grants(api_id, client_id) ON DELETE CASCADE
) USING heap;

CREATE INDEX IF NOT EXISTS idx_grant_permissions_client ON public.oidc_client_api_grant_permissions (client_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS public.oidc_client_api_grant_permissions;
DROP TABLE IF EXISTS public.oidc_client_api_grants;
DROP TABLE IF EXISTS public.api_permissions;
DROP TABLE IF EXISTS public.apis;
-- +goose StatementEnd
