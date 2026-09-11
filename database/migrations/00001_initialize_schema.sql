-- +goose Up
-- +goose StatementBegin
SET timezone = 'UTC';

-- ============================================================================
-- Register some PosgreSQL extensions
-- ============================================================================
-- CREATE EXTENSION IF NOT EXISTS pg_stat_statements; -- Track planning and execution statistics of all SQL statements executed
CREATE EXTENSION IF NOT EXISTS citext;    -- Case-insensitive text type (slower than varchar or text columns)
CREATE EXTENSION IF NOT EXISTS hstore;    -- Key-Value pairs for storing unstructured data
CREATE EXTENSION IF NOT EXISTS pg_trgm;   -- Text similarity measurement and index searching based on trigrams
CREATE EXTENSION IF NOT EXISTS pgcrypto;  -- Cryptographic functions for hashing and encryption
CREATE EXTENSION IF NOT EXISTS plpgsql;   -- PL/pgSQL procedural language

-- ============================================================================
-- Create additional schemas for application use. Avoid circular dependency
-- between schemas! Specify schema owner for better security and audit.
-- User `pg_database_owner` means the owner of the database, typically the user who created it.
-- Grant USAGE privileges to PUBLIC is optional, only if you want to allow all users to access the schema.
-- Query to list all schemas in the database:
--   SELECT * FROM pg_namespace WHERE nspname NOT IN ('pg_catalog', 'pg_toast', 'information_schema');
-- ============================================================================
DO $$
BEGIN
    -- Create schema for reference data (country, city, currency, etc.)
    IF NOT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'auth') THEN
        EXECUTE 'CREATE SCHEMA auth AUTHORIZATION pg_database_owner';
        EXECUTE 'GRANT USAGE, CREATE ON SCHEMA auth TO pg_database_owner;';
        -- EXECUTE 'GRANT USAGE ON SCHEMA auth TO PUBLIC;';
    END IF;
    -- Create schema for reference data (country, city, currency, etc.)
    IF NOT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'reference') THEN
        EXECUTE 'CREATE SCHEMA reference AUTHORIZATION pg_database_owner';
        EXECUTE 'GRANT USAGE, CREATE ON SCHEMA reference TO pg_database_owner;';
        -- EXECUTE 'GRANT USAGE ON SCHEMA reference TO PUBLIC;';
    END IF;
    -- Create schema for scheduler or background jobs (queue)
    IF NOT EXISTS (SELECT 1 FROM pg_namespace WHERE nspname = 'scheduler') THEN
        EXECUTE 'CREATE SCHEMA scheduler AUTHORIZATION pg_database_owner';
        EXECUTE 'GRANT USAGE, CREATE ON SCHEMA scheduler TO pg_database_owner;';
        -- EXECUTE 'GRANT USAGE ON SCHEMA scheduler TO PUBLIC;';
    END IF;
END$$;

-- ============================================================================
-- Create auto-update function, fill updated_at column automatically.
-- CURRENT_TIMESTAMP similar to timezone('utc'::text, now())::timestamptz
-- ============================================================================
CREATE OR REPLACE FUNCTION fn_updated_at_value()
RETURNS TRIGGER AS $$ BEGIN NEW.updated_at = CURRENT_TIMESTAMP; RETURN NEW; END; $$
LANGUAGE plpgsql;

-- ============================================================================
-- Returns the size of each database in bytes, KB, MB, and GB.
-- Example: SELECT * FROM get_database_sizes();
-- Specific: SELECT * FROM get_database_sizes() WHERE db_name = 'postgres';
-- ============================================================================
CREATE OR REPLACE FUNCTION get_database_sizes()
RETURNS TABLE (db_name TEXT, size_in_bytes TEXT, size_in_kb TEXT, size_in_mb TEXT, size_in_gb TEXT)
AS $$
BEGIN
    RETURN QUERY
    WITH db_sizes AS (
        SELECT datname::TEXT AS db_name, pg_database_size(datname) AS raw_size
        FROM pg_database WHERE datname NOT IN ('template0', 'template1')
    ),
    all_databases AS (SELECT 'all_database'::TEXT AS db_name, sum(raw_size) AS raw_size FROM db_sizes)
    SELECT
        cd.db_name,
        CASE
            WHEN cd.raw_size % 1 = 0 THEN cd.raw_size::text
            ELSE ROUND(cd.raw_size::numeric, 4)::text
        END AS size_in_bytes,
        CASE
            WHEN cd.raw_size % 1024 = 0 THEN (cd.raw_size / 1024)::text
            ELSE ROUND((cd.raw_size / 1024)::numeric, 4)::text
        END AS size_in_kb,
        CASE
            WHEN cd.raw_size % (1024 * 1024) = 0 THEN (cd.raw_size / (1024 * 1024))::text
            ELSE ROUND((cd.raw_size / (1024 * 1024))::numeric, 2)::text
        END AS size_in_mb,
        CASE
            WHEN cd.raw_size % (1024 * 1024 * 1024) = 0 THEN (cd.raw_size / (1024 * 1024 * 1024))::text
            ELSE ROUND((cd.raw_size / (1024 * 1024 * 1024))::numeric, 4)::text
        END AS size_in_gb
    FROM (SELECT * FROM db_sizes UNION ALL SELECT * FROM all_databases) AS cd
    ORDER BY cd.db_name ASC;
END;
$$ LANGUAGE plpgsql STABLE;

-- ============================================================================
-- Returns size information of user-defined tables including bytes, KB, MB, GB.
-- Example: SELECT * FROM get_table_sizes();
-- ============================================================================
CREATE OR REPLACE FUNCTION get_table_sizes()
RETURNS TABLE ( schema_name TEXT, table_name TEXT, size_in_bytes TEXT, size_in_kb TEXT, size_in_mb TEXT, size_in_gb TEXT)
AS $$
BEGIN
    RETURN QUERY
    WITH table_sizes AS (
        SELECT
            nsp.nspname::TEXT AS schema_name,
            cls.relname::TEXT AS table_name,
            pg_total_relation_size(cls.oid) AS raw_size
        FROM pg_class cls JOIN pg_namespace nsp ON nsp.oid = cls.relnamespace
        WHERE cls.relkind = 'r'
          AND nsp.nspname NOT IN ('pg_catalog', 'information_schema')
          AND cls.relname <> 'app_migration'
    )
    SELECT ts.schema_name, ts.table_name,
        CASE -- bytes
            WHEN ts.raw_size % 1 = 0 THEN ts.raw_size::TEXT
            ELSE ROUND(ts.raw_size::NUMERIC, 4)::TEXT
        END AS size_in_bytes,
        CASE -- kilobytes
            WHEN ts.raw_size % 1024 = 0 THEN (ts.raw_size / 1024)::TEXT
            ELSE ROUND((ts.raw_size / 1024)::NUMERIC, 4)::TEXT
        END AS size_in_kb,
        CASE -- megabytes
            WHEN ts.raw_size % (1024 * 1024) = 0 THEN (ts.raw_size / (1024 * 1024))::TEXT
            ELSE ROUND((ts.raw_size / (1024 * 1024))::NUMERIC, 2)::TEXT
        END AS size_in_mb,
        CASE -- gigabytes
            WHEN ts.raw_size % (1024 * 1024 * 1024) = 0 THEN (ts.raw_size / (1024 * 1024 * 1024))::TEXT
            ELSE ROUND((ts.raw_size / (1024 * 1024 * 1024))::NUMERIC, 4)::TEXT
        END AS size_in_gb
    FROM table_sizes ts
    ORDER BY ts.table_name ASC;
END;
$$ LANGUAGE plpgsql STABLE;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop custom functions but keep the extensions.
DROP FUNCTION IF EXISTS get_table_sizes();
DROP FUNCTION IF EXISTS get_database_sizes();
DROP FUNCTION IF EXISTS fn_updated_at_value();

-- Revoke privileges from users before dropping schemas to ensure clean removal and avoid dependency issues.
REVOKE USAGE, CREATE ON SCHEMA reference FROM pg_database_owner;
REVOKE USAGE, CREATE ON SCHEMA scheduler FROM pg_database_owner;

-- Use CASCADE only if you are sure all objects should be removed.
-- Use RESTRICT to avoid accidental data loss; schema will only be dropped if empty.
DROP SCHEMA IF EXISTS reference RESTRICT;
DROP SCHEMA IF EXISTS scheduler CASCADE;

-- +goose StatementEnd
