-- Preserve period, source_record, archive_entry and other source metadata.
-- Apply after 001_rag_schema.sql (and 002 for previously created schemas).
BEGIN;
ALTER TABLE rag.documents
    ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb;
COMMIT;
