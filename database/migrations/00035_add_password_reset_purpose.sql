-- +goose Up
-- +goose StatementBegin

-- Password-reset tokens share the auth_tokens store; widen the
-- purpose CHECK to admit the new value.

ALTER TABLE public.auth_tokens
    DROP CONSTRAINT IF EXISTS chk_auth_token_purpose;

ALTER TABLE public.auth_tokens
    ADD CONSTRAINT chk_auth_token_purpose
    CHECK (purpose IN ('email_verification', 'one_time_access', 'reauthentication', 'password_reset'));

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Reset tokens were only added with this migration; any row still
-- using the new purpose must be dropped before restoring the old
-- CHECK.

DELETE FROM public.auth_tokens WHERE purpose = 'password_reset';

ALTER TABLE public.auth_tokens
    DROP CONSTRAINT IF EXISTS chk_auth_token_purpose;

ALTER TABLE public.auth_tokens
    ADD CONSTRAINT chk_auth_token_purpose
    CHECK (purpose IN ('email_verification', 'one_time_access', 'reauthentication'));

-- +goose StatementEnd
