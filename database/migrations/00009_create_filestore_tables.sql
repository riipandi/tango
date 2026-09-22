-- +goose Up
-- +goose StatementBegin

-- --------------------------------------------------------
-- Table: public.storage_files (one row per stored file; the manifest
-- the chunk diff and the re-upload decision read)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.storage_files (
    id UUID NOT NULL PRIMARY KEY DEFAULT uuidv7(),
    key TEXT NOT NULL,
    size BIGINT NOT NULL DEFAULT 0,
    chunk_size INTEGER NOT NULL,
    chunk_count INTEGER NOT NULL DEFAULT 0,
    -- SHA-256 over the chunk hashes in order, so a manifest comparison is
    -- one hash read before any chunk row is touched.
    content_hash TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    -- Free-form metadata the storing feature owns: content type, original
    -- file name, owner id. The engine never reads it, only carries it.
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    -- Upload progress: the chunk count the current upload round has
    -- already put in the backend.
    chunks_done INTEGER NOT NULL DEFAULT 0,
    -- The staging fingerprint the current chunk list was computed from:
    -- size plus modification time. A retry that finds both unchanged
    -- reuses the stored chunk list instead of hashing the file again.
    staging_size BIGINT NOT NULL DEFAULT 0,
    staging_mtime TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp(),
    UNIQUE (key),
    CONSTRAINT chk_storage_files_status CHECK (status IN ('pending', 'ready', 'failed')),
    CONSTRAINT chk_storage_files_chunk_size CHECK (chunk_size > 0),
    CONSTRAINT chk_storage_files_size CHECK (size >= 0),
    CONSTRAINT chk_storage_files_chunks_done CHECK (chunks_done >= 0)
) USING heap;

CREATE INDEX IF NOT EXISTS idx_storage_files_status ON public.storage_files (status);
CREATE INDEX IF NOT EXISTS idx_storage_files_content_hash ON public.storage_files (content_hash);

-- --------------------------------------------------------
-- Table: public.storage_chunks (the chunk manifest of one file; the
-- content hash doubles as the object address in the backend)
-- --------------------------------------------------------

CREATE TABLE IF NOT EXISTS public.storage_chunks (
    file_id UUID NOT NULL REFERENCES public.storage_files (id) ON DELETE CASCADE,
    chunk_index INTEGER NOT NULL,
    hash TEXT NOT NULL,
    size INTEGER NOT NULL,
    uploaded_at TIMESTAMPTZ,
    PRIMARY KEY (file_id, chunk_index),
    CONSTRAINT chk_storage_chunks_hash CHECK (hash ~ '^[0-9a-f]{64}$'),
    CONSTRAINT chk_storage_chunks_size CHECK (size > 0)
) USING heap;

CREATE INDEX IF NOT EXISTS idx_storage_chunks_hash ON public.storage_chunks (hash);

CREATE OR REPLACE FUNCTION fn_update_storage_files_updated_at()
RETURNS TRIGGER AS $$
BEGIN
  NEW.updated_at = clock_timestamp(); RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_storage_files_updated_at
    BEFORE UPDATE ON public.storage_files
    FOR EACH ROW EXECUTE FUNCTION fn_update_storage_files_updated_at();

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TRIGGER IF EXISTS trg_storage_files_updated_at ON public.storage_files;
DROP FUNCTION IF EXISTS fn_update_storage_files_updated_at();

DROP TABLE IF EXISTS public.storage_chunks;
DROP TABLE IF EXISTS public.storage_files;

-- +goose StatementEnd
