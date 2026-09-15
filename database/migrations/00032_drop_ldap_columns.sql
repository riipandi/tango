-- +goose Up
-- +goose StatementBegin

-- LDAP synchronization was removed; the directory-controlled
-- identity key no longer has a writer.
DROP INDEX IF EXISTS idx_users_ldap_id;
ALTER TABLE public.users DROP COLUMN IF EXISTS ldap_id;

DROP INDEX IF EXISTS user_groups_ldap_id;
ALTER TABLE public.user_groups DROP COLUMN IF EXISTS ldap_id;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
ALTER TABLE public.users
    ADD COLUMN IF NOT EXISTS ldap_id TEXT DEFAULT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_ldap_id ON public.users USING btree (ldap_id);

ALTER TABLE public.user_groups
    ADD COLUMN IF NOT EXISTS ldap_id TEXT;
CREATE UNIQUE INDEX IF NOT EXISTS user_groups_ldap_id ON public.user_groups USING btree (ldap_id);
-- +goose StatementEnd
