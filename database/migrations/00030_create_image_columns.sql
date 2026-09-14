-- +goose Up
-- +goose StatementBegin

-- Blob paths for the phase 9C image surfaces. NULL = fall back to
-- the default (bundled profile picture / no logo).
ALTER TABLE public.users
    ADD COLUMN IF NOT EXISTS profile_picture_path TEXT;

ALTER TABLE public.oidc_clients
    ADD COLUMN IF NOT EXISTS logo_path TEXT;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE public.oidc_clients DROP COLUMN IF EXISTS logo_path;
ALTER TABLE public.users DROP COLUMN IF EXISTS profile_picture_path;
-- +goose StatementEnd
