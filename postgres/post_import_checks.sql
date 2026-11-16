-- Run with psql -v ON_ERROR_STOP=1 -v collection_name=avito-habr -f post_import_checks.sql
-- If --collection-name was supplied during import, pass that name instead.

SELECT col.name,
       count(DISTINCT d.id) AS documents,
       count(c.id) AS chunks
FROM rag.collections col
LEFT JOIN rag.documents d ON d.collection_id=col.id AND d.is_active
LEFT JOIN rag.chunks c ON c.doc_id=d.id
WHERE col.name=:'collection_name'
GROUP BY col.name;

SELECT CASE
         WHEN d.source_key LIKE 'playbook/%' THEN 'playbook'
         WHEN d.source_key LIKE 'internship/%' THEN 'checklist'
         ELSE 'habr'
       END AS source_group,
       count(DISTINCT d.id) AS documents,
       count(c.id) AS chunks
FROM rag.documents d
JOIN rag.collections col ON col.id=d.collection_id
LEFT JOIN rag.chunks c ON c.doc_id=d.id
WHERE col.name=:'collection_name' AND d.is_active
GROUP BY source_group
ORDER BY source_group;

SELECT count(*) FILTER (
         WHERE public.vector_dims(c.embedding)<>1024
            OR public.vector_norm(c.embedding) NOT BETWEEN 0.999 AND 1.001
       ) AS invalid_vectors,
       min(public.vector_norm(c.embedding)) AS min_norm,
       max(public.vector_norm(c.embedding)) AS max_norm,
       count(*) FILTER (WHERE c.fts IS NULL) AS missing_fts
FROM rag.chunks c
JOIN rag.documents d ON d.id=c.doc_id
JOIN rag.collections col ON col.id=d.collection_id
WHERE col.name=:'collection_name' AND d.is_active;

SELECT d.source_key,count(*) AS duplicates
FROM rag.documents d
JOIN rag.collections col ON col.id=d.collection_id
WHERE col.name=:'collection_name'
GROUP BY d.source_key
HAVING count(*)>1;

SELECT d.source_key,c.position,count(*) AS duplicates
FROM rag.chunks c
JOIN rag.documents d ON d.id=c.doc_id
JOIN rag.collections col ON col.id=d.collection_id
WHERE col.name=:'collection_name'
GROUP BY d.source_key,c.position
HAVING count(*)>1;

SELECT d.source_key,d.title,
       d.metadata->>'period' AS period,
       d.metadata->>'source_record' AS source_record
FROM rag.documents d
JOIN rag.collections col ON col.id=d.collection_id
WHERE col.name=:'collection_name' AND d.source_key LIKE 'internship/%'
ORDER BY d.source_key
LIMIT 5;

-- Self-match checks that cosine search works. It is not a retrieval-quality evaluation.
WITH q AS (
    SELECT c.embedding
    FROM rag.chunks c
    JOIN rag.documents d ON d.id=c.doc_id
    JOIN rag.collections col ON col.id=d.collection_id
    WHERE col.name=:'collection_name' AND d.is_active
      AND d.min_role='intern' AND d.team IS NULL
    ORDER BY c.id
    LIMIT 1
)
SELECT d.title,c.section,1-(c.embedding <=> q.embedding) AS similarity
FROM rag.chunks c
JOIN rag.documents d ON d.id=c.doc_id
JOIN rag.collections col ON col.id=d.collection_id
CROSS JOIN q
WHERE col.name=:'collection_name' AND d.is_active
  AND d.min_role='intern' AND d.team IS NULL
ORDER BY c.embedding <=> q.embedding,c.id
LIMIT 6;
