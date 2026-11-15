package rag

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	for _, name := range []string{
		"001_rag_schema.sql",
		"002_optional_token_count.sql",
		"003_provided_origin.sql",
		"004_document_metadata.sql",
	} {
		body, err := migrations.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		if _, err = conn.Exec(ctx, string(body)); err != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = conn.Exec(cleanup, "ROLLBACK")
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

type Store struct {
	pool         *pgxpool.Pool
	config       Config
	collectionID int64
}

// OpenStore verifies the immutable indexing profile. The caller owns the pool.
func OpenStore(ctx context.Context, pool *pgxpool.Pool, config Config) (*Store, error) {
	if pool == nil {
		return nil, fmt.Errorf("database pool is required")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	_, err := pool.Exec(ctx, `INSERT INTO rag.collections
		(name,embedding_model,embedding_revision,embedding_dimension,embedding_text_version,query_prompt_name,chunking_config)
		VALUES ($1,$2,NULLIF($3,''),1024,1,'query',$4::jsonb) ON CONFLICT (name) DO NOTHING`,
		config.Collection, config.EmbeddingModel, config.EmbeddingRevision, string(config.ChunkingConfig))
	if err != nil {
		return nil, fmt.Errorf("create collection (apply migrations first): %w", err)
	}
	s := &Store{pool: pool, config: config}
	err = pool.QueryRow(ctx, `SELECT id FROM rag.collections WHERE name=$1 AND embedding_model=$2
		AND embedding_revision IS NOT DISTINCT FROM NULLIF($3,'') AND embedding_dimension=1024
		AND embedding_text_version=1 AND query_prompt_name='query' AND chunking_config=$4::jsonb`,
		config.Collection, config.EmbeddingModel, config.EmbeddingRevision, string(config.ChunkingConfig)).Scan(&s.collectionID)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("collection profile mismatch: create a new collection and reindex")
	}
	if err != nil {
		return nil, fmt.Errorf("read collection: %w", err)
	}
	return s, nil
}

// Document describes one source; its prepared passages are passed separately as []string.
type Document struct {
	SourceKey  string   `json:"source_key"`
	Title      string   `json:"title"`
	Text       string   `json:"text"`
	SourceURL  string   `json:"source_url"`
	SourcePath string   `json:"source_path"`
	Origin     string   `json:"origin"`
	MinRole    string   `json:"min_role"`
	Team       string   `json:"team"`
	Tags       []string `json:"tags"`
}

func (d Document) validate() error {
	if strings.TrimSpace(d.SourceKey) == "" || strings.TrimSpace(d.Title) == "" || strings.TrimSpace(d.SourceURL) == "" {
		return fmt.Errorf("source_key, title and source_url are required")
	}
	if _, err := roleRank(d.MinRole); err != nil {
		return err
	}
	if d.Origin != "public" && d.Origin != "synthetic" && d.Origin != "provided" {
		return fmt.Errorf("origin must be public, synthetic or provided")
	}
	if d.Team != "" && strings.TrimSpace(d.Team) == "" {
		return fmt.Errorf("team must not be whitespace")
	}
	return nil
}

func hashText(text string) string {
	hash := sha256.Sum256([]byte(text))
	return hex.EncodeToString(hash[:])
}

// SaveDocument embeds already separated passages, with title prefix, before opening a transaction.
func (s *Store) SaveDocument(ctx context.Context, d Document, texts []string, embedder Embedder) (int64, error) {
	if err := d.validate(); err != nil {
		return 0, err
	}
	if len(texts) == 0 || embedder == nil {
		return 0, fmt.Errorf("nonempty texts and embedder are required")
	}
	embeddingTexts := make([]string, len(texts))
	for i, text := range texts {
		if strings.TrimSpace(text) == "" {
			return 0, fmt.Errorf("text %d is empty", i)
		}
		embeddingTexts[i] = d.Title + "\n" + text
	}
	vectors, err := embedder.EmbedDocuments(ctx, embeddingTexts)
	if err != nil {
		return 0, fmt.Errorf("embed document: %w", err)
	}
	return s.save(ctx, d, texts, make([]string, len(texts)), make([]*int, len(texts)), vectors)
}

// Parallel slices are private storage parameters, not a second chunk domain model.
func (s *Store) save(ctx context.Context, d Document, texts, sections []string, tokenCounts []*int, vectors [][]float32) (int64, error) {
	if err := d.validate(); err != nil {
		return 0, err
	}
	if len(texts) == 0 || len(texts) != len(sections) || len(texts) != len(tokenCounts) || len(texts) != len(vectors) {
		return 0, fmt.Errorf("document arrays must have equal nonzero lengths")
	}
	embeddingTexts := make([]string, len(texts))
	literals := make([]string, len(texts))
	for i, text := range texts {
		if strings.TrimSpace(text) == "" || (tokenCounts[i] != nil && *tokenCounts[i] <= 0) {
			return 0, fmt.Errorf("invalid text or token count at position %d", i+1)
		}
		embeddingTexts[i] = d.Title
		if strings.TrimSpace(sections[i]) != "" {
			embeddingTexts[i] += " | " + sections[i]
		}
		embeddingTexts[i] += "\n" + text
		v, err := normalize(vectors[i])
		if err != nil {
			return 0, fmt.Errorf("position %d: %w", i+1, err)
		}
		literals[i] = vectorLiteral(v)
	}
	contentHash := hashText(canonicalDocument(d.Title, d.Text))
	if d.Tags == nil {
		d.Tags = []string{}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = tx.Rollback(cleanup)
	}()
	// UPSERT acquires the document row lock, serializing writers of one source.
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO rag.documents
		(collection_id,source_key,source_path,title,content,source_url,origin,min_role,team,tags,content_hash)
		VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,NULLIF($9,''),$10,$11)
		ON CONFLICT (collection_id,source_key) DO UPDATE SET
		source_path=EXCLUDED.source_path,title=EXCLUDED.title,content=EXCLUDED.content,
		source_url=EXCLUDED.source_url,origin=EXCLUDED.origin,min_role=EXCLUDED.min_role,
		team=EXCLUDED.team,tags=EXCLUDED.tags,content_hash=EXCLUDED.content_hash,is_active=true,updated_at=now()
		RETURNING id`, s.collectionID, d.SourceKey, d.SourcePath, d.Title, d.Text, d.SourceURL, d.Origin, d.MinRole, d.Team, d.Tags, contentHash).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("save document: %w", err)
	}
	if _, err = tx.Exec(ctx, `DELETE FROM rag.chunks WHERE doc_id=$1`, id); err != nil {
		return 0, err
	}
	for i, text := range texts {
		_, err = tx.Exec(ctx, `INSERT INTO rag.chunks(doc_id,position,section,text,embedding_text,token_count,content_hash,embedding)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8::text::public.vector(1024))`,
			id, i+1, sections[i], text, embeddingTexts[i], tokenCounts[i], hashText(embeddingTexts[i]), literals[i])
		if err != nil {
			return 0, fmt.Errorf("save position %d: %w", i+1, err)
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

func (s *Store) DeactivateDocument(ctx context.Context, sourceKey string) error {
	_, err := s.pool.Exec(ctx, `UPDATE rag.documents SET is_active=false,updated_at=now() WHERE collection_id=$1 AND source_key=$2`, s.collectionID, sourceKey)
	return err
}

// Matches json.dumps([title,text], ensure_ascii=False, separators=(',', ':')),
// including U+2028/U+2029, which encoding/json always escapes.
func canonicalDocument(title, text string) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, s := range []string{title, text} {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteByte('"')
		for _, r := range s {
			switch r {
			case '"', '\\':
				b.WriteByte('\\')
				b.WriteRune(r)
			case '\b':
				b.WriteString(`\b`)
			case '\f':
				b.WriteString(`\f`)
			case '\n':
				b.WriteString(`\n`)
			case '\r':
				b.WriteString(`\r`)
			case '\t':
				b.WriteString(`\t`)
			default:
				if r < 0x20 {
					fmt.Fprintf(&b, `\u%04x`, r)
				} else {
					b.WriteRune(r)
				}
			}
		}
		b.WriteByte('"')
	}
	b.WriteByte(']')
	return b.String()
}
