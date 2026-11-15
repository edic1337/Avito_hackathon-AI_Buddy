-- Go accepts prepared []string and has no tokenizer. Unknown is NULL, not an
-- invented token count. Existing Python exports retain their exact token_count.
ALTER TABLE rag.chunks ALTER COLUMN token_count DROP NOT NULL;
