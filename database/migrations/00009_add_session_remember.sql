-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Column: public.sessions.remember
-- --------------------------------------------------------

-- Records the duration the caller asked for at sign-in. The sliding
-- refresh needs it: without the flag, refreshing an active short
-- session would silently promote it to the long lifetime.
ALTER TABLE public.sessions
    ADD COLUMN IF NOT EXISTS remember BOOLEAN NOT NULL DEFAULT false;

-- --------------------------------------------------------
-- Column: public.user_mfa_pending.remember
-- --------------------------------------------------------

-- The second-factor bridge outlives the sign-in call, so it carries
-- the requested duration forward to the session issued after the
-- factor is verified.
ALTER TABLE public.user_mfa_pending
    ADD COLUMN IF NOT EXISTS remember BOOLEAN NOT NULL DEFAULT false;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

ALTER TABLE public.user_mfa_pending DROP COLUMN IF EXISTS remember;
ALTER TABLE public.sessions DROP COLUMN IF EXISTS remember;

-- +goose StatementEnd
