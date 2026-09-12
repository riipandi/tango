-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Table: public.users
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.users (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    username CITEXT NOT NULL UNIQUE,
    email TEXT NOT NULL UNIQUE CHECK (email ~* '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$'),
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    display_name TEXT NOT NULL CHECK (char_length(display_name) > 0),
    avatar_url TEXT,
    locale TEXT,
    is_admin BOOLEAN NOT NULL DEFAULT FALSE,
    disabled BOOLEAN NOT NULL DEFAULT FALSE,
    ldap_id TEXT DEFAULT NULL,
    metadata JSONB DEFAULT NULL, -- Metadata can containg user-specific information
    email_verified_at TIMESTAMPTZ DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL,
    last_login_at TIMESTAMPTZ DEFAULT NULL,
    banned_at TIMESTAMPTZ DEFAULT NULL,
    ban_expires TIMESTAMPTZ DEFAULT NULL,
    ban_reason TEXT DEFAULT NULL,
    -- Username only allows alphanumeric characters and underscores, must be between 3 and 32 characters long
    CONSTRAINT chk_username_format CHECK (username IS NULL OR username ~ '^[a-zA-Z0-9_]{3,32}$')
) USING heap;

-- Create trigger for updated_at column and soft delete
CREATE TRIGGER trg_users_updated_at BEFORE UPDATE ON public.users FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();
CREATE TRIGGER trg_users_deleted_record AFTER DELETE ON public.users FOR EACH ROW EXECUTE FUNCTION fn_soft_delete();

-- Indexes for `public.users` table
CREATE INDEX IF NOT EXISTS idx_users_display_name ON public.users USING gin (display_name gin_trgm_ops);
CREATE INDEX IF NOT EXISTS idx_users_username ON public.users USING GIN (username gin_trgm_ops) WHERE username IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_banned_at ON public.users (banned_at) WHERE banned_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_ban_expires ON public.users (ban_expires) WHERE ban_expires IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_banned_expires ON public.users (banned_at, ban_expires) WHERE banned_at IS NOT NULL AND ban_expires IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_created_at ON public.users (created_at);
CREATE INDEX IF NOT EXISTS idx_users_created_at ON public.users (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_users_updated_at ON public.users (updated_at) WHERE updated_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_users_metadata_gin ON public.users USING GIN (metadata);
CREATE INDEX IF NOT EXISTS idx_users_last_login_at ON public.users (last_login_at) WHERE last_login_at IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_normalized_username ON public.users (LOWER(username));
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_ldap_id ON public.users USING btree (ldap_id);

-- Insert a default admin user for testing purposes
INSERT INTO public.users (id, first_name, last_name, display_name, email, username, avatar_url, is_admin, email_verified_at, created_at)
VALUES (
    uuidv7(),
    'Admin',
    'Sistem',
    'Admin Sistem',
    'admin@example.com',
    'admin',
    'https://api.dicebear.com/7.x/avataaars/svg?seed=Admin+Sistem',
    TRUE,
    CURRENT_TIMESTAMP,
    CURRENT_TIMESTAMP
)
ON CONFLICT (username) DO NOTHING;

-- --------------------------------------------------------
-- Table: public.user_passwords (junction table)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.user_passwords (
    user_id UUID NOT NULL PRIMARY KEY REFERENCES public.users(id) ON DELETE CASCADE,
    password_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL,
    CONSTRAINT user_passwords_one_per_user UNIQUE (user_id) -- Ensure only one password per user
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_user_passwords_updated_at BEFORE UPDATE ON public.user_passwords FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- Indexes for `public.user_passwords` table
CREATE INDEX IF NOT EXISTS idx_user_passwords_user_id ON public.user_passwords (user_id);
CREATE INDEX IF NOT EXISTS idx_user_passwords_created_at ON public.user_passwords (created_at);
CREATE INDEX IF NOT EXISTS idx_user_passwords_updated_at ON public.user_passwords (updated_at) WHERE updated_at IS NOT NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop triggers in reverse order
DROP TRIGGER IF EXISTS trg_user_passwords_updated_at ON public.user_passwords;
DROP TRIGGER IF EXISTS trg_users_deleted_record on public.users;
DROP TRIGGER IF EXISTS trg_users_updated_at on public.users;

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_user_passwords_updated_at;
DROP INDEX IF EXISTS idx_user_passwords_created_at;
DROP INDEX IF EXISTS idx_user_passwords_user_id;
DROP INDEX IF EXISTS idx_users_ldap_id;
DROP INDEX IF EXISTS idx_users_normalized_username;
DROP INDEX IF EXISTS idx_users_last_login_at;
DROP INDEX IF EXISTS idx_users_metadata_gin;
DROP INDEX IF EXISTS idx_users_updated_at;
DROP INDEX IF EXISTS idx_users_created_at;
DROP INDEX IF EXISTS idx_users_created_at;
DROP INDEX IF EXISTS idx_users_banned_expires;
DROP INDEX IF EXISTS idx_users_ban_expires;
DROP INDEX IF EXISTS idx_users_banned_at;
DROP INDEX IF EXISTS idx_users_username;
DROP INDEX IF EXISTS idx_users_display_name;

-- Drop tables in reverse order of creation (tables with FKs first)
DROP TABLE IF EXISTS public.user_passwords;
DROP TABLE IF EXISTS public.users;

-- +goose StatementEnd
