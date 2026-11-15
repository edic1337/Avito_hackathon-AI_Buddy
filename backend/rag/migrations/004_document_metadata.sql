-- Preserve structured source metadata from schema_version=2 corpus exports.
BEGIN;
ALTER TABLE rag.documents
    ADD COLUMN IF NOT EXISTS metadata JSONB NOT NULL DEFAULT '{}'::jsonb;
COMMIT;
