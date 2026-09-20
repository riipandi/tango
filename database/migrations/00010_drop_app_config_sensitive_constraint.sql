-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Constraint: drop public.app_config.chk_app_config_sensitive_enc
-- --------------------------------------------------------

-- The SMTP relay password is environment-only, so no catalog key is
-- sensitive and no stored value may be sealed. The constraint would
-- reject a legitimate plaintext write to a key that no longer exists.
ALTER TABLE public.app_config
    DROP CONSTRAINT IF EXISTS chk_app_config_sensitive_enc;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE public.app_config
    ADD CONSTRAINT chk_app_config_sensitive_enc
        CHECK (key <> 'smtp_password' OR value LIKE 'enc:%');

-- +goose StatementEnd
