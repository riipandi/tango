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

-- --------------------------------------------------------
-- Table: public.user_groups
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.user_groups (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    name TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL CHECK (char_length(display_name) > 0),
    ldap_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_user_groups_updated_at BEFORE UPDATE ON public.user_groups FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

CREATE UNIQUE INDEX IF NOT EXISTS user_groups_ldap_id ON public.user_groups USING btree (ldap_id);

-- --------------------------------------------------------
-- Table: public.user_groups_users (junction table)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.user_groups_users (
    user_id UUID NOT NULL,
    user_group_id UUID NOT NULL,
    PRIMARY KEY (user_id, user_group_id),
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    FOREIGN KEY (user_group_id) REFERENCES user_groups(id) ON DELETE CASCADE
) USING heap;

CREATE INDEX IF NOT EXISTS idx_user_groups_users_user_id ON public.user_groups_users USING btree (user_id);
CREATE INDEX IF NOT EXISTS idx_user_groups_users_user_group_id ON public.user_groups_users USING btree (user_group_id);

-- --------------------------------------------------------
-- Table: public.user_phones
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.user_phones (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    phone_number TEXT NOT NULL UNIQUE, -- Example: +6281234567890
    use_for_mfa BOOLEAN NOT NULL DEFAULT FALSE,
    use_for_sign_in BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL,
    verified_at TIMESTAMPTZ DEFAULT NULL,
    -- Phone number must be between 8 and 20 characters long and can only contain numbers and "+"
    CONSTRAINT chk_phone_format CHECK (phone_number IS NULL OR phone_number ~ '^\+?[0-9]{8,20}$'),
    CONSTRAINT user_phones_one_per_user UNIQUE (user_id) -- Ensure only one phone per user
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_user_phones_updated_at BEFORE UPDATE ON public.user_phones FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- Indexes for `public.user_phones` table (ensure only one primary phone per user)
CREATE INDEX IF NOT EXISTS idx_user_phones_user_id ON public.user_phones (user_id);
CREATE INDEX IF NOT EXISTS idx_user_phones_phone_number ON public.user_phones (phone_number);
CREATE INDEX IF NOT EXISTS idx_user_phones_verified_at ON public.user_phones (verified_at);
CREATE INDEX IF NOT EXISTS idx_user_phones_use_for_mfa ON public.user_phones (user_id) WHERE use_for_mfa = TRUE;
CREATE INDEX IF NOT EXISTS idx_user_phones_use_for_sign_in ON public.user_phones (user_id) WHERE use_for_sign_in = TRUE;

-- --------------------------------------------------------
-- Table: public.sessions
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.sessions (
    id TEXT NOT NULL PRIMARY KEY UNIQUE,
    user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    provider TEXT NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    user_agent TEXT,
    device_name TEXT,
    device_fingerprint TEXT,
    ip_address INET,
    totp_pending BOOLEAN NOT NULL DEFAULT FALSE,
    oauth_groups TEXT NULL,
    oauth_name TEXT NULL,
    oauth_sub TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > CURRENT_TIMESTAMP),
    refreshed_at TIMESTAMPTZ DEFAULT NULL,
    revoked_at TIMESTAMPTZ DEFAULT NULL,
    revoked_by UUID REFERENCES public.users(id) ON DELETE SET NULL,
    impersonated_by UUID REFERENCES public.users(id) ON DELETE SET NULL
) USING heap;

-- Indexes for `public.sessions` table
CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON public.sessions (user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON public.sessions (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_user_id_expires_at ON public.sessions USING btree (user_id, expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_ip_address ON public.sessions (ip_address) WHERE ip_address IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_device_fingerprint ON public.sessions (device_fingerprint);
CREATE INDEX IF NOT EXISTS idx_sessions_impersonated_by ON public.sessions (impersonated_by) WHERE impersonated_by IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_sessions_oauth_sub ON public.sessions USING btree (oauth_sub);
CREATE UNIQUE INDEX IF NOT EXISTS idx_sessions_token_hash ON public.sessions (token_hash);

-- --------------------------------------------------------
-- Table: public.auth_tokens
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.auth_tokens (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL,
    token_hash TEXT NOT NULL UNIQUE,
    device_token VARCHAR(16), -- Used only for one_time_access
    purpose TEXT NOT NULL DEFAULT 'one_time_access',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > CURRENT_TIMESTAMP),
    last_sent_at TIMESTAMPTZ DEFAULT NULL,
    FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE,
    CONSTRAINT chk_auth_token_purpose CHECK (purpose IN ('email_verification', 'one_time_access', 'reauthentication'))
) USING heap;

-- Indexes for `public.auth_tokens` table
CREATE INDEX IF NOT EXISTS idx_auth_tokens_user_id ON public.auth_tokens (user_id);
CREATE INDEX IF NOT EXISTS idx_auth_tokens_purpose ON public.auth_tokens (purpose);
CREATE INDEX IF NOT EXISTS idx_auth_tokens_expires_at ON public.auth_tokens USING btree (expires_at);
CREATE INDEX IF NOT EXISTS idx_auth_tokens_token_hash ON public.auth_tokens USING HASH (token_hash);
CREATE UNIQUE INDEX IF NOT EXISTS idx_auth_tokens_user_id_purpose ON public.auth_tokens USING btree (user_id, purpose);

-- --------------------------------------------------------
-- Table: public.signup_tokens
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.signup_tokens (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    token_hash TEXT NOT NULL UNIQUE,
    usage_limit INTEGER NOT NULL DEFAULT 1,
    usage_count INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL
) USING heap;

-- Indexes for `public.signup_tokens` table
CREATE INDEX IF NOT EXISTS idx_signup_tokens_expires_at ON public.signup_tokens USING btree (expires_at);

-- --------------------------------------------------------
-- Table: public.signup_tokens_user_groups (junction table)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.signup_tokens_user_groups (
    signup_token_id UUID NOT NULL,
    user_group_id UUID NOT NULL,
    PRIMARY KEY (signup_token_id, user_group_id),
    FOREIGN KEY (signup_token_id) REFERENCES signup_tokens(id) ON DELETE CASCADE,
    FOREIGN KEY (user_group_id) REFERENCES user_groups(id) ON DELETE CASCADE
) USING heap;

-- --------------------------------------------------------
-- Table: public.refresh_tokens
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.refresh_tokens (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL REFERENCES public.users(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL REFERENCES public.sessions(id) ON DELETE CASCADE,
    token_hash BYTEA NOT NULL UNIQUE,
    ip_address INET,
    user_agent TEXT,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > CURRENT_TIMESTAMP),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    revoked_at TIMESTAMPTZ DEFAULT NULL,
    revoked_by UUID REFERENCES public.users(id) ON DELETE SET NULL
) USING heap;

-- Indexes for `public.refresh_tokens` table
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_id ON public.refresh_tokens (user_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_session_id ON public.refresh_tokens (session_id);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_expires_at ON public.refresh_tokens (expires_at);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_revoked_at ON public.refresh_tokens (revoked_at) WHERE revoked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_created_at ON public.refresh_tokens (created_at);
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_ip_address ON public.refresh_tokens (ip_address) WHERE ip_address IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_refresh_tokens_user_agent ON public.refresh_tokens (user_agent) WHERE user_agent IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_refresh_tokens_token_hash ON public.refresh_tokens (token_hash);

-- --------------------------------------------------------
-- Table: public.audit_logs — track user actions and system events
-- --------------------------------------------------------

DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'audit_action_status') THEN
    CREATE TYPE public.audit_action_status AS ENUM ('success', 'failed', 'pending', 'unknown');
END IF; END$$;

DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'audit_event_trigger') THEN
    CREATE TYPE public.audit_event_trigger AS ENUM ('user', 'system', 'external');
END IF; END$$;

CREATE TABLE IF NOT EXISTS public.audit_logs (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    event TEXT NOT NULL,
    trigger_type public.audit_event_trigger NOT NULL,
    action_status public.audit_action_status DEFAULT 'pending',
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    ip_address INET,
    user_agent TEXT,
    country TEXT,
    city TEXT,
    resource_type TEXT,
    resource_id UUID,
    user_id UUID REFERENCES public.users(id) ON DELETE SET NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
) USING heap;

CREATE INDEX IF NOT EXISTS idx_audit_logs_client_name ON public.audit_logs USING btree (((payload ->> 'client_name'::text)));
CREATE INDEX IF NOT EXISTS idx_audit_logs_country ON public.audit_logs USING btree (country);
CREATE INDEX IF NOT EXISTS idx_audit_logs_created_at ON public.audit_logs USING btree (created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_logs_event ON public.audit_logs USING btree (event);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_agent ON public.audit_logs USING btree (user_agent);
CREATE INDEX IF NOT EXISTS idx_audit_logs_user_id ON public.audit_logs USING btree (user_id);

-- --------------------------------------------------------
-- Table: public.invitations
-- --------------------------------------------------------

DO $$ BEGIN IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'invitation_status') THEN
    CREATE TYPE public.invitation_status AS ENUM ('pending', 'accepted', 'expired', 'revoked');
END IF; END$$;

CREATE TABLE IF NOT EXISTS public.invitations (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    inviter_id UUID NOT NULL REFERENCES public.users (id) ON DELETE CASCADE,
    email TEXT NOT NULL CHECK (email ~* '^[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}$'),
    status public.invitation_status NOT NULL DEFAULT 'pending',
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > CURRENT_TIMESTAMP),
    confirmed_at TIMESTAMPTZ DEFAULT NULL CHECK (confirmed_at > CURRENT_TIMESTAMP),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL
) USING heap;

-- Create trigger for updated_at column and soft delete
CREATE TRIGGER trg_invitations_updated_at BEFORE UPDATE ON public.invitations FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- Indexes for `public.invitations` table
CREATE INDEX IF NOT EXISTS idx_invitations_inviter_id ON public.invitations (inviter_id);
CREATE INDEX IF NOT EXISTS idx_invitations_email ON public.invitations (email);
CREATE INDEX IF NOT EXISTS idx_invitations_status ON public.invitations (status);
CREATE INDEX IF NOT EXISTS idx_invitations_expires_at ON public.invitations (expires_at);
CREATE INDEX IF NOT EXISTS idx_invitations_confirmed_at ON public.invitations (confirmed_at);
CREATE INDEX IF NOT EXISTS idx_invitations_status_expires ON public.invitations (status, expires_at);

-- --------------------------------------------------------
-- Table: public.mfa_keys
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.mfa_keys (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID NOT NULL REFERENCES public.users (id) ON DELETE CASCADE,
    device_name TEXT NOT NULL,
    device_type TEXT NOT NULL CHECK (device_type IN ('authenticator', 'email', 'sms', 'whatsapp')),
    secret_key TEXT NOT NULL CHECK (LENGTH(secret_key) >= 16), -- encrypted (AES256)
    backup_codes JSONB NOT NULL DEFAULT '[]'::jsonb, -- hashed backup codes
    verified_at TIMESTAMPTZ DEFAULT NULL,
    last_used_at TIMESTAMPTZ DEFAULT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_mfa_keys_updated_at BEFORE UPDATE ON public.mfa_keys FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- Indexes for `public.mfa_keys` table (GIN index on backup_codes used to speed up containment queries)
CREATE INDEX IF NOT EXISTS idx_mfa_keys_backup_codes_gin ON public.mfa_keys USING GIN (backup_codes);
CREATE INDEX IF NOT EXISTS idx_mfa_keys_user_id ON public.mfa_keys (user_id);
CREATE INDEX IF NOT EXISTS idx_mfa_keys_updated_at ON public.mfa_keys (updated_at) WHERE updated_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_mfa_keys_last_used_at ON public.mfa_keys (last_used_at) WHERE last_used_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_mfa_keys_verified_at ON public.mfa_keys (verified_at) WHERE verified_at IS NOT NULL;

-- --------------------------------------------------------
-- Table: public.api_keys (consider using rate limiting)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.api_keys (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    user_id UUID REFERENCES public.users(id) ON DELETE CASCADE,
    name TEXT NOT NULL CHECK (char_length(name) > 0),
    prefix VARCHAR(10) NOT NULL CHECK (char_length(prefix) > 0),
    key_hash BYTEA NOT NULL,
    description TEXT,
    expiration_email_sent_at TIMESTAMPTZ DEFAULT NULL,
    expires_at TIMESTAMPTZ NOT NULL CHECK (expires_at > CURRENT_TIMESTAMP),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL,
    last_used_at TIMESTAMPTZ DEFAULT NULL,
    revoked_at TIMESTAMPTZ DEFAULT NULL,
    CONSTRAINT chk_name_format CHECK (name ~ '^[a-zA-Z0-9_ -]{3,64}$')
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_api_keys_updated_at BEFORE UPDATE ON public.api_keys FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- Indexes for `public.api_keys` table
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON public.api_keys USING btree (user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_expires_at ON public.api_keys (expires_at) WHERE expires_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id_expires_at ON public.api_keys USING btree (user_id, expires_at);
CREATE INDEX IF NOT EXISTS idx_api_keys_revoked_at ON public.api_keys (revoked_at) WHERE revoked_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_last_used_at ON public.api_keys (last_used_at) WHERE last_used_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_api_keys_created_at ON public.api_keys (created_at);
CREATE INDEX IF NOT EXISTS idx_api_keys_updated_at ON public.api_keys (updated_at) WHERE updated_at IS NOT NULL;

-- Unique constraint: name must be unique per user (including NULL user_id for global keys)
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_name_user_id ON public.api_keys USING btree (name, COALESCE(user_id, '00000000-0000-0000-0000-000000000000'::UUID));

-- --------------------------------------------------------
-- Table: public.oauth_connections (social sign-in connections)
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
DROP TRIGGER IF EXISTS trg_api_keys_updated_at ON public.api_keys;
DROP TRIGGER IF EXISTS trg_mfa_keys_updated_at ON public.mfa_keys;
DROP TRIGGER IF EXISTS trg_invitations_updated_at ON public.invitations;
DROP TRIGGER IF EXISTS trg_user_phones_updated_at ON public.user_phones;
DROP TRIGGER IF EXISTS trg_user_groups_updated_at on public.user_groups;
DROP TRIGGER IF EXISTS trg_user_passwords_updated_at ON public.user_passwords;
DROP TRIGGER IF EXISTS trg_users_deleted_record on public.users;
DROP TRIGGER IF EXISTS trg_users_updated_at on public.users;

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_api_keys_name_user_id;
DROP INDEX IF EXISTS idx_api_keys_updated_at;
DROP INDEX IF EXISTS idx_api_keys_created_at;
DROP INDEX IF EXISTS idx_api_keys_last_used_at;
DROP INDEX IF EXISTS idx_api_keys_revoked_at;
DROP INDEX IF EXISTS idx_api_keys_user_id_expires_at;
DROP INDEX IF EXISTS idx_api_keys_expires_at;
DROP INDEX IF EXISTS idx_api_keys_user_id;
DROP INDEX IF EXISTS idx_mfa_keys_backup_codes_gin;
DROP INDEX IF EXISTS idx_mfa_keys_user_id;
DROP INDEX IF EXISTS idx_mfa_keys_updated_at;
DROP INDEX IF EXISTS idx_mfa_keys_last_used_at;
DROP INDEX IF EXISTS idx_mfa_keys_verified_at;
DROP INDEX IF EXISTS idx_invitations_status_expires;
DROP INDEX IF EXISTS idx_invitations_confirmed_at;
DROP INDEX IF EXISTS idx_invitations_expires_at;
DROP INDEX IF EXISTS idx_invitations_status;
DROP INDEX IF EXISTS idx_invitations_email;
DROP INDEX IF EXISTS idx_invitations_inviter_id;
DROP INDEX IF EXISTS idx_audit_logs_user_id;
DROP INDEX IF EXISTS idx_audit_logs_user_agent;
DROP INDEX IF EXISTS idx_audit_logs_event;
DROP INDEX IF EXISTS idx_audit_logs_created_at;
DROP INDEX IF EXISTS idx_audit_logs_country;
DROP INDEX IF EXISTS idx_audit_logs_client_name;
DROP INDEX IF EXISTS idx_refresh_tokens_token_hash;
DROP INDEX IF EXISTS idx_refresh_tokens_user_agent;
DROP INDEX IF EXISTS idx_refresh_tokens_ip_address;
DROP INDEX IF EXISTS idx_refresh_tokens_created_at;
DROP INDEX IF EXISTS idx_refresh_tokens_revoked_at;
DROP INDEX IF EXISTS idx_refresh_tokens_expires_at;
DROP INDEX IF EXISTS idx_refresh_tokens_session_id;
DROP INDEX IF EXISTS idx_refresh_tokens_user_id;
DROP INDEX IF EXISTS idx_signup_tokens_expires_at;
DROP INDEX IF EXISTS idx_auth_tokens_user_id_purpose;
DROP INDEX IF EXISTS idx_auth_tokens_token_hash;
DROP INDEX IF EXISTS idx_auth_tokens_expires_at;
DROP INDEX IF EXISTS idx_auth_tokens_purpose;
DROP INDEX IF EXISTS idx_auth_tokens_user_id;
DROP INDEX IF EXISTS idx_sessions_token_hash;
DROP INDEX IF EXISTS idx_sessions_oauth_sub;
DROP INDEX IF EXISTS idx_sessions_impersonated_by;
DROP INDEX IF EXISTS idx_sessions_device_fingerprint;
DROP INDEX IF EXISTS idx_sessions_ip_address;
DROP INDEX IF EXISTS idx_sessions_user_id_expires_at;
DROP INDEX IF EXISTS idx_sessions_expires_at;
DROP INDEX IF EXISTS idx_sessions_user_id;
DROP INDEX IF EXISTS idx_user_phones_use_for_sign_in;
DROP INDEX IF EXISTS idx_user_phones_use_for_mfa;
DROP INDEX IF EXISTS idx_user_phones_verified_at;
DROP INDEX IF EXISTS idx_user_phones_phone_number;
DROP INDEX IF EXISTS idx_user_phones_user_id;
DROP INDEX IF EXISTS idx_user_groups_users_user_group_id;
DROP INDEX IF EXISTS idx_user_groups_users_user_id;
DROP INDEX IF EXISTS user_groups_ldap_id;
DROP INDEX IF EXISTS idx_users_ldap_id;
DROP INDEX IF EXISTS idx_users_normalized_username;
DROP INDEX IF EXISTS idx_users_last_login_at;
DROP INDEX IF EXISTS idx_users_metadata_gin;
DROP INDEX IF EXISTS idx_users_updated_at;
DROP INDEX IF EXISTS idx_users_created_at;
DROP INDEX IF EXISTS idx_users_banned_expires;
DROP INDEX IF EXISTS idx_users_ban_expires;
DROP INDEX IF EXISTS idx_users_banned_at;
DROP INDEX IF EXISTS idx_users_username;
DROP INDEX IF EXISTS idx_users_display_name;

-- Drop tables in reverse order of creation (tables with FKs first)
DROP TABLE IF EXISTS public.oauth_connections;
DROP TABLE IF EXISTS public.api_keys;
DROP TABLE IF EXISTS public.mfa_keys;
DROP TABLE IF EXISTS public.invitations;
DROP TABLE IF EXISTS public.refresh_tokens;
DROP TABLE IF EXISTS public.signup_tokens_user_groups;
DROP TABLE IF EXISTS public.signup_tokens;
DROP TABLE IF EXISTS public.auth_tokens;
DROP TABLE IF EXISTS public.sessions;
DROP TABLE IF EXISTS public.audit_logs;
DROP TABLE IF EXISTS public.user_phones;
DROP TABLE IF EXISTS public.user_groups_users;
DROP TABLE IF EXISTS public.user_groups;
DROP TABLE IF EXISTS public.user_passwords;
DROP TABLE IF EXISTS public.users;

-- Drop the custom enum types
DROP TYPE IF EXISTS public.audit_action_status;
DROP TYPE IF EXISTS public.audit_event_trigger;
DROP TYPE IF EXISTS public.invitation_status;

-- +goose StatementEnd
