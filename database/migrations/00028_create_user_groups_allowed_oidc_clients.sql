-- +goose Up
-- +goose StatementBegin

-- Group-side client allowlist (upstream PUT /user-groups/{id}/allowed-oidc-clients):
-- which relying-party clients a user group may be granted to. This is the inverse of
-- public.oidc_clients_allowed_user_groups (client-side group restriction).
CREATE TABLE IF NOT EXISTS public.user_groups_allowed_oidc_clients (
    user_group_id UUID NOT NULL,
    oidc_client_id TEXT NOT NULL,
    PRIMARY KEY (user_group_id, oidc_client_id),
    FOREIGN KEY (user_group_id) REFERENCES public.user_groups(id) ON DELETE CASCADE,
    FOREIGN KEY (oidc_client_id) REFERENCES public.oidc_clients(id) ON DELETE CASCADE
) USING heap;

CREATE INDEX IF NOT EXISTS idx_user_groups_allowed_oidc_clients_client_id
    ON public.user_groups_allowed_oidc_clients USING btree (oidc_client_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_user_groups_allowed_oidc_clients_client_id;
DROP TABLE IF EXISTS public.user_groups_allowed_oidc_clients;
-- +goose StatementEnd
