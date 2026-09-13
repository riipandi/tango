-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Table: public.device_login_requests (QR / cross-device sign-in
-- state; upstream keeps this in an actor framework — a plain table
-- is the portable equivalent)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.device_login_requests (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    code TEXT NOT NULL UNIQUE,             -- short user code (P + 7 chars)
    device_token_hash TEXT NOT NULL,       -- binds the polling device
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'denied', 'consumed')),
    user_id UUID REFERENCES public.users (id) ON DELETE CASCADE,
    ip_address TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at TIMESTAMPTZ NOT NULL
) USING heap;

CREATE INDEX IF NOT EXISTS idx_device_login_requests_expires_at ON public.device_login_requests (expires_at);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS public.device_login_requests;
-- +goose StatementEnd
