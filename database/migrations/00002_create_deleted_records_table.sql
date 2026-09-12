-- +goose Up
-- +goose StatementBegin

-- ============================================================================
-- Soft Delete Implementation Using "Deleted Record Insert" Pattern.
--
-- When a record is deleted from any table with a trigger, the deleted data
-- is automatically captured as JSONB in the `deleted_records` table.
-- @see https://brandur.org/fragments/deleted-record-insert
-- ============================================================================

CREATE TABLE IF NOT EXISTS public.deleted_records (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    object_id UUID NOT NULL, -- ID for the object (PK)
    source_table VARCHAR(200) NOT NULL,
    data JSONB NOT NULL DEFAULT '{}'::jsonb,
    deleted_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT NULL
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_deleted_records_updated_at BEFORE UPDATE ON public.deleted_records FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- Settings table indexes and updated_at trigger
CREATE INDEX IF NOT EXISTS idx_deleted_records_object_id ON public.deleted_records (object_id);
CREATE INDEX IF NOT EXISTS idx_deleted_records_source_table ON public.deleted_records (source_table);
CREATE INDEX IF NOT EXISTS idx_deleted_records_updated_at ON public.deleted_records (updated_at) WHERE updated_at IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_deleted_records_deleted_at ON public.deleted_records (deleted_at) WHERE deleted_at IS NOT NULL;

-- Create trigger function for deleted record insert
CREATE OR REPLACE FUNCTION fn_soft_delete()
RETURNS TRIGGER AS $$
    BEGIN
        EXECUTE 'INSERT INTO public.deleted_records (object_id, source_table, data) VALUES ($1, $2, $3)'
        USING OLD.id, TG_TABLE_NAME, to_jsonb(OLD.*);
        RETURN OLD;
    END;
$$
LANGUAGE plpgsql;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop trigger before dropping the table
DROP TRIGGER IF EXISTS trg_deleted_records_updated_at ON public.deleted_records;

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_deleted_records_deleted_at;
DROP INDEX IF EXISTS idx_deleted_records_updated_at;
DROP INDEX IF EXISTS idx_deleted_records_source_table;
DROP INDEX IF EXISTS idx_deleted_records_object_id;

-- Drop the table itself
DROP TABLE IF EXISTS public.deleted_records;

-- Drop the trigger function
DROP FUNCTION IF EXISTS fn_soft_delete();

-- +goose StatementEnd
