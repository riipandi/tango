-- +goose Up
-- +goose StatementBegin

-- Recoverable secrets are always sealed by pkg/crypto and stored in
-- the canonical "enc:<ciphertext>" form; the database rejects any
-- unprefixed value.

ALTER TABLE public.webhook_events
    ADD CONSTRAINT chk_webhook_secret_enc CHECK (secret LIKE 'enc:%');

ALTER TABLE public.scim_service_providers
    ADD CONSTRAINT chk_scim_token_enc CHECK (token LIKE 'enc:%');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE public.webhook_events DROP CONSTRAINT IF EXISTS chk_webhook_secret_enc;
ALTER TABLE public.scim_service_providers DROP CONSTRAINT IF EXISTS chk_scim_token_enc;
-- +goose StatementEnd
