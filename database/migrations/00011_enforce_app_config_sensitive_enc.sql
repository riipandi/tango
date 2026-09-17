-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Enforce the enc: marker for sensitive settings stored in
-- app_config. Rows holding an un-encryptable plaintext value (written
-- before the seal existed) are removed: the env-backed default still
-- provides the setting.
-- --------------------------------------------------------

DELETE FROM public.app_config WHERE key = 'smtp_password' AND value NOT LIKE 'enc:%';

ALTER TABLE public.app_config
    ADD CONSTRAINT chk_app_config_sensitive_enc
    CHECK (key <> 'smtp_password' OR value LIKE 'enc:%');

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE public.app_config DROP CONSTRAINT IF EXISTS chk_app_config_sensitive_enc;

-- +goose StatementEnd
