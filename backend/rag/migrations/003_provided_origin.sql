-- Allow source documents explicitly provided for the onboarding corpus.
-- This does not change document access roles or team restrictions.
BEGIN;
ALTER TABLE rag.documents DROP CONSTRAINT IF EXISTS documents_origin_check;
ALTER TABLE rag.documents ADD CONSTRAINT documents_origin_check
    CHECK (origin IN ('public', 'synthetic', 'provided'));
COMMIT;
