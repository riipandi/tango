-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Drop the obsolete oidc_clients columns: the single-secret mirror
-- (superseded by the credentials JSONB list) and the image type
-- columns (logo presence derives from logo_path).
-- --------------------------------------------------------

ALTER TABLE public.oidc_clients DROP COLUMN IF EXISTS secret;
ALTER TABLE public.oidc_clients DROP COLUMN IF EXISTS image_type;
ALTER TABLE public.oidc_clients DROP COLUMN IF EXISTS dark_image_type;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE public.oidc_clients ADD COLUMN image_type VARCHAR(10);
ALTER TABLE public.oidc_clients ADD COLUMN dark_image_type TEXT;
ALTER TABLE public.oidc_clients ADD COLUMN secret TEXT;

-- +goose StatementEnd
