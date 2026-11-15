-- Run once as the migration owner after both rag migrations have completed.
-- This grants runtime access only; it does not create or change the RAG schema.
SELECT 'CREATE ROLE avito_buddy_app LOGIN'
WHERE NOT EXISTS (
    SELECT 1 FROM pg_roles WHERE rolname = 'avito_buddy_app'
) \gexec

GRANT CONNECT ON DATABASE avito_buddy TO avito_buddy_app;
GRANT USAGE ON SCHEMA rag TO avito_buddy_app;
GRANT SELECT, INSERT, UPDATE, DELETE
    ON ALL TABLES IN SCHEMA rag TO avito_buddy_app;
GRANT USAGE, SELECT, UPDATE
    ON ALL SEQUENCES IN SCHEMA rag TO avito_buddy_app;
GRANT USAGE ON TYPE public.vector TO avito_buddy_app;

-- Applies to future objects created by the role running this script.
ALTER DEFAULT PRIVILEGES IN SCHEMA rag
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO avito_buddy_app;
ALTER DEFAULT PRIVILEGES IN SCHEMA rag
    GRANT USAGE, SELECT, UPDATE ON SEQUENCES TO avito_buddy_app;
