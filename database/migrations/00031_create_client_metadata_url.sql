-- +goose Up
-- +goose StatementBegin

-- CIMD-lite: the metadata document URL a client materializes from.
-- Upstream uses the URL itself as the client id; tango keeps typeid
-- ids and stores the source URL here.
ALTER TABLE public.oidc_clients
    ADD COLUMN IF NOT EXISTS metadata_url TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE public.oidc_clients DROP COLUMN IF EXISTS metadata_url;
-- +goose StatementEnd
