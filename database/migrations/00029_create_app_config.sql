-- +goose Up
-- +goose StatementBegin

-- Admin-editable application configuration: key/value rows override
-- the env-backed defaults (upstream app_config; tango trims the key
-- set — SMTP/LDAP stay env-only).
CREATE TABLE IF NOT EXISTS public.app_config (
    key TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
) USING heap;

CREATE TRIGGER trg_app_config_updated_at
    BEFORE UPDATE ON public.app_config
    FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TRIGGER IF EXISTS trg_app_config_updated_at ON public.app_config;
DROP TABLE IF EXISTS public.app_config;
-- +goose StatementEnd
