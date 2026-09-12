-- +goose Up
-- +goose StatementBegin

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

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop triggers in reverse order
DROP TRIGGER IF EXISTS trg_user_phones_updated_at ON public.user_phones;

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_user_phones_use_for_sign_in;
DROP INDEX IF EXISTS idx_user_phones_use_for_mfa;
DROP INDEX IF EXISTS idx_user_phones_verified_at;
DROP INDEX IF EXISTS idx_user_phones_phone;
DROP INDEX IF EXISTS idx_user_phones_user_id;

-- Drop the table
DROP TABLE IF EXISTS public.user_phones;

-- +goose StatementEnd
