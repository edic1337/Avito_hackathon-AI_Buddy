package rag

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Both ranking branches see only accessible rows. Exact cosine search is
// intentional for a small corpus; adding ANN indexes requires recall evaluation.
const searchSQL = `WITH accessible AS MATERIALIZED (
	SELECT c.id,c.text,c.section,c.fts,c.embedding,d.title,d.source_url,d.origin
	FROM rag.chunks c JOIN rag.documents d ON d.id=c.doc_id
	WHERE d.collection_id=$1 AND d.is_active
	AND (CASE d.min_role WHEN 'intern' THEN 1 WHEN 'buddy' THEN 2 WHEN 'manager' THEN 3 END)<=$2
	AND (d.team IS NULL OR d.team=NULLIF($3,''))
), scored AS MATERIALIZED (
	SELECT *,1-(embedding <=> $4::text::public.vector(1024)) AS cosine FROM accessible
), vector_top AS (
	SELECT id,cosine FROM scored ORDER BY cosine DESC,id LIMIT $6
), vector_ranks AS (
	SELECT id,row_number() OVER (ORDER BY cosine DESC,id) AS rank FROM vector_top
), text_top AS (
	SELECT id,ts_rank_cd(fts,websearch_to_tsquery('russian',$5)) AS score
	FROM scored WHERE fts @@ websearch_to_tsquery('russian',$5)
	ORDER BY score DESC,id LIMIT $6
), text_ranks AS (
	SELECT id,row_number() OVER (ORDER BY score DESC,id) AS rank FROM text_top
), fused AS (
	SELECT COALESCE(v.id,t.id) AS id,
	COALESCE(1.0/(60+v.rank),0)+COALESCE(1.0/(60+t.rank),0) AS score
	FROM vector_ranks v FULL OUTER JOIN text_ranks t ON v.id=t.id
)
SELECT s.title,s.section,s.text,s.source_url,s.origin
FROM fused f JOIN scored s ON s.id=f.id WHERE s.cosine >= $7
ORDER BY f.score DESC,s.cosine DESC,s.id LIMIT $8`

func (s *Store) Search(ctx context.Context, question string, vector []float32, access Access) ([]string, error) {
	rank, err := roleRank(access.Role)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(question) == "" {
		return nil, fmt.Errorf("question must not be empty")
	}
	v, err := normalize(vector)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, searchSQL, s.collectionID, rank, access.Team, vectorLiteral(v), question, s.config.CandidateLimit, s.config.MinSimilarity, s.config.TopK)
	if err != nil {
		return nil, fmt.Errorf("search knowledge: %w", err)
	}
	defer rows.Close()
	result := make([]string, 0, s.config.TopK)
	remaining := s.config.MaxContextRunes
	for rows.Next() {
		var title, section, body, source, origin string
		if err = rows.Scan(&title, &section, &body, &source, &origin); err != nil {
			return nil, err
		}
		label := "Открытый источник"
		if origin == "synthetic" {
			label = "Синтетическая тестовая инструкция, не регламент Авито"
		}
		header := title
		if section != "" {
			header += " | " + section
		}
		text := fmt.Sprintf("%s\nПроисхождение: %s\nИсточник: %s\n\n%s", header, label, source, body)
		// Do not truncate a process midway and accidentally omit a required step.
		if size := utf8.RuneCountInString(text); size <= remaining {
			result = append(result, text)
			remaining -= size
		}
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("read search results: %w", err)
	}
	return result, nil
}

type Searcher interface {
	Search(context.Context, string, []float32, Access) ([]string, error)
}

type Retriever struct {
	embedder Embedder
	store    Searcher
}

func NewRetriever(embedder Embedder, store Searcher) (*Retriever, error) {
	if embedder == nil || store == nil {
		return nil, fmt.Errorf("embedder and store are required")
	}
	return &Retriever{embedder, store}, nil
}

func (r *Retriever) Retrieve(ctx context.Context, question string, access Access) ([]string, error) {
	if _, err := roleRank(access.Role); err != nil {
		return nil, err
	}
	if strings.TrimSpace(question) == "" {
		return nil, fmt.Errorf("question must not be empty")
	}
	vector, err := r.embedder.EmbedQuery(ctx, question)
	if err != nil {
		return nil, fmt.Errorf("embed question: %w", err)
	}
	return r.store.Search(ctx, question, vector, access)
}
