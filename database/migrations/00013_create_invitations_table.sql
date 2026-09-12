-- +goose Up
-- +goose StatementBegin

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

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop trigger first
DROP TRIGGER IF EXISTS trg_invitations_updated_at ON public.invitations;

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_invitations_status_expires;
DROP INDEX IF EXISTS idx_invitations_confirmed_at;
DROP INDEX IF EXISTS idx_invitations_expires_at;
DROP INDEX IF EXISTS idx_invitations_status;
DROP INDEX IF EXISTS idx_invitations_email;
DROP INDEX IF EXISTS idx_invitations_inviter_id;

-- Drop the table
DROP TABLE IF EXISTS public.invitations;

-- Drop the custom enum type
DROP TYPE IF EXISTS public.invitation_status;

-- +goose StatementEnd
