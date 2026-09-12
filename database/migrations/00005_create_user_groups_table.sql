-- +goose Up
-- +goose StatementBegin

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

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop triggers in reverse order
DROP TRIGGER IF EXISTS trg_user_groups_updated_at on public.user_groups;

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS user_groups_ldap_id;
DROP INDEX IF EXISTS idx_user_groups_users_user_group_id;
DROP INDEX IF EXISTS idx_user_groups_users_user_id;

-- Drop tables in reverse order of creation (tables with FKs first)
DROP TABLE IF EXISTS public.user_groups_users;
DROP TABLE IF EXISTS public.user_groups;

-- +goose StatementEnd
