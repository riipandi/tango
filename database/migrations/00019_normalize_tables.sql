-- +goose Up
-- +goose StatementBegin

-- Normalize Unicode strings to NFC (Canonical Composition) form to ensure consistent string comparison
-- and prevent duplicate entries. NFC combines base characters with combining marks into single precomposed
-- characters (e.g., "café" from different sources may have different byte representations: e+◌́ vs é).
-- Note: normalize() function requires PostgreSQL 13+ and UTF8 encoding

DO $$
BEGIN
    IF current_setting('server_encoding') = 'UTF8' THEN
        -- Normalize application configuration
        UPDATE app_settings SET "value" = normalize("value", NFC) WHERE "name" = 'app_name';

        -- Normalize user identifiers and personal info (critical for deduplication and authentication)
        UPDATE users SET
            username = normalize(username, NFC),
            email = normalize(email, NFC),
            first_name = normalize(first_name, NFC),
            last_name = normalize(last_name, NFC);

        -- Normalize user group identifiers
        UPDATE user_groups SET display_name = normalize(display_name, NFC), "name" = normalize("name", NFC);

        -- Normalize API key metadata
        UPDATE api_keys SET name = normalize(name, NFC), description = normalize(description, NFC);

        -- Normalize custom claim keys and values
        UPDATE custom_claims SET "key" = normalize("key", NFC), "value" = normalize("value", NFC);

        -- Normalize OIDC client names
        UPDATE oidc_clients SET name = normalize(name, NFC);
    ELSE
        RAISE NOTICE 'Skipping normalization: server_encoding is %', current_setting('server_encoding');
    END IF;
END;
$$ LANGUAGE plpgsql;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- No rollback needed: normalization is idempotent and only affects internal representation
SELECT 'no-op';

-- +goose StatementEnd
