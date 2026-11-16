-- Only needed if 001_rag_schema.sql was applied before origin=provided existed.
-- This migration does not change document access roles or team restrictions.
BEGIN;
ALTER TABLE rag.documents DROP CONSTRAINT IF EXISTS documents_origin_check;
ALTER TABLE rag.documents ADD CONSTRAINT documents_origin_check
    CHECK (origin IN ('public', 'synthetic', 'provided'));
COMMIT;
