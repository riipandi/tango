-- +goose Up
-- +goose StatementBegin

-- Optional: add `file_store_references` table to track file usage across the application

-- --------------------------------------------------------
-- Table: public.file_stores used for storing S3 file metadata
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.file_stores (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    key_path TEXT NOT NULL UNIQUE,
    file_size BIGINT NOT NULL,
    file_hash TEXT NOT NULL,
    original_name TEXT NOT NULL,
    stored_name TEXT NOT NULL,
    content_type TEXT NOT NULL,
    metadata JSONB DEFAULT '{}'::jsonb, -- Additional metadata (EXIF, alt text, captions, etc.)
    s3_uri TEXT, -- Additional for S3 storage
    etag VARCHAR(100), -- Additional for S3 storage
    bucket_name TEXT, -- Additional for S3 storage
    bucket_access VARCHAR(20) DEFAULT 'private' CHECK (bucket_access IN ('public', 'private')), -- Additional for S3 storage
    storage_class VARCHAR(50) DEFAULT 'STANDARD', -- Additional for S3 storage
    uploaded_by UUID REFERENCES public.users(id) ON DELETE CASCADE,
    uploaded_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ DEFAULT NULL,
    deleted_at TIMESTAMPTZ DEFAULT NULL, -- Soft delete (NULL = active, NOT NULL = deleted)
    deleted_by UUID REFERENCES public.users(id) ON DELETE SET NULL
) USING heap;

-- Create trigger for updated_at column
CREATE TRIGGER trg_file_stores_updated_at BEFORE UPDATE ON public.file_stores FOR EACH ROW EXECUTE FUNCTION fn_updated_at_value();

-- Optimized indexes for common query patterns
CREATE UNIQUE INDEX IF NOT EXISTS idx_file_stores_key_path ON public.file_stores (key_path);
CREATE INDEX IF NOT EXISTS idx_file_stores_uploader_active ON public.file_stores (uploaded_by, deleted_at) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_file_stores_bucket_access ON public.file_stores (bucket_access) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_file_stores_file_hash ON public.file_stores (file_hash) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_file_stores_uploaded_at ON public.file_stores (uploaded_at DESC) WHERE deleted_at IS NULL;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Drop triggers in reverse order
DROP TRIGGER IF EXISTS trg_file_stores_updated_at on public.file_stores;

-- Drop indexes in reverse order of creation
DROP INDEX IF EXISTS idx_file_stores_uploaded_at;
DROP INDEX IF EXISTS idx_file_stores_file_hash;
DROP INDEX IF EXISTS idx_file_stores_bucket_access;
DROP INDEX IF EXISTS idx_file_stores_uploader_active;
DROP INDEX IF EXISTS idx_file_stores_key_path;

-- Drop tables in reverse order of creation (tables with FKs first)
DROP TABLE IF EXISTS public.file_stores;

-- +goose StatementEnd
