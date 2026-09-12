-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Table: public.app_settings
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.app_settings (
    name TEXT NOT NULL PRIMARY KEY,
    value TEXT NOT NULL, -- use prefix `enc:` for encrypted value
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL,
    -- Setting name only allows lowercase alphanumeric characters and underscores
    CONSTRAINT chk_name_format CHECK (name IS NULL OR name ~ '^[a-z0-9_]+$')
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_app_settings_updated_at BEFORE UPDATE ON public.app_settings FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- Indexes for `public.app_settings` table
CREATE INDEX IF NOT EXISTS idx_app_settings_name ON public.app_settings (name);
CREATE INDEX IF NOT EXISTS idx_app_settings_created_at ON public.app_settings (created_at);
CREATE INDEX IF NOT EXISTS idx_app_settings_updated_at ON public.app_settings (updated_at) WHERE updated_at IS NOT NULL;

-- Insert some default settings into the table
INSERT INTO public.app_settings (name, value) VALUES
    ('auth_method_email_password_enabled', 'true'),
    ('auth_method_username_password_enabled', 'true'),
    ('auth_method_email_otp_enabled', 'true'),
    ('auth_method_social_google_enabled', 'false'),
    ('auth_mfa_force_enabled', 'false')
ON CONFLICT (name) DO NOTHING;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop the trigger first
DROP TRIGGER IF EXISTS trg_app_settings_updated_at on public.app_settings;

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_app_settings_updated_at;
DROP INDEX IF EXISTS idx_app_settings_created_at;
DROP INDEX IF EXISTS idx_app_settings_name;

-- Drop the table
DROP TABLE IF EXISTS public.app_settings;

-- +goose StatementEnd
