-- +goose Up
-- +goose StatementBegin

-- Final-schema cleanup: remove tables and columns with no live code
-- readers, then align session identifiers with the UUID storage rule
-- (TypeID forms exist only at the store edge).

DROP TABLE IF EXISTS public.refresh_tokens;
DROP TABLE IF EXISTS public.oauth_connections;
DROP TABLE IF EXISTS public.mfa_keys;
DROP TABLE IF EXISTS public.invitations;
DROP TABLE IF EXISTS public.user_phones;
DROP TABLE IF EXISTS public.file_stores;
DROP TABLE IF EXISTS public.oidc_device_codes;

-- The duplicate legacy settings shape; app_config is the one final
-- settings table.
DROP TABLE IF EXISTS public.app_settings;

ALTER TABLE public.sessions DROP COLUMN IF EXISTS totp_pending;
ALTER TABLE public.sessions DROP COLUMN IF EXISTS oauth_groups;
ALTER TABLE public.sessions DROP COLUMN IF EXISTS oauth_name;
ALTER TABLE public.sessions DROP COLUMN IF EXISTS oauth_sub;

-- Session ids become UUIDs: refresh_tokens (the only TEXT FK) is
-- dropped above, and the store edge converts to and from the TypeID
-- form.
ALTER TABLE public.sessions
    ALTER COLUMN id DROP DEFAULT,
    ALTER COLUMN id TYPE uuid USING id::uuid,
    ALTER COLUMN id SET DEFAULT uuidv7();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- Not reversible: the dropped tables and columns hold no live data
-- contract, and the TEXT session ids cannot be reconstructed.
-- +goose StatementEnd
