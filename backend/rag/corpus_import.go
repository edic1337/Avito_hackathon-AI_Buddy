package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// CorpusJSON imports the schema_version=2 corpus export produced by the RAG
// preparation pipeline. Documents are decoded one at a time to keep memory use
// bounded for the large Habr export.
func ImportCorpusJSON(ctx context.Context, pool *pgxpool.Pool, paths []string, collectionOverride string, expectedDocuments, expectedChunks int, progress io.Writer) error {
	if pool == nil {
		return fmt.Errorf("database pool is required")
	}
	if len(paths) == 0 {
		return fmt.Errorf("at least one corpus JSON file is required")
	}
	if expectedDocuments < 1 || expectedChunks < 1 {
		return fmt.Errorf("expected document and chunk counts must be positive")
	}

	seenKeys := make(map[string]string, expectedDocuments)
	var profile *corpusCollection
	totalDocuments, totalChunks := 0, 0
	fileStats := make(map[string]os.FileInfo, len(paths))

	// Validate every file completely before the first database write.
	for _, path := range paths {
		p, summary, documents, chunks, err := scanCorpusJSON(path, func(p corpusCollection, d corpusDocument) error {
			if err := validateCorpusDocument(p, d); err != nil {
				return fmt.Errorf("source_key %q: %w", d.SourceKey, err)
			}
			if previous, ok := seenKeys[d.SourceKey]; ok {
				return fmt.Errorf("duplicate source_key %q in %s and %s", d.SourceKey, previous, path)
			}
			seenKeys[d.SourceKey] = path
			return nil
		})
		if err != nil {
			return err
		}
		if err := validateCorpusCollection(p); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if profile == nil {
			copy := p
			profile = &copy
		} else if !sameCorpusCollection(*profile, p) {
			return fmt.Errorf("collection profile differs between corpus files")
		}
		if !summary.Complete || summary.ExpectedDocuments != documents || summary.Documents != documents || summary.Chunks != chunks || len(summary.Errors) != 0 {
			return fmt.Errorf("%s: incomplete export or summary mismatch (expected=%d documents=%d chunks=%d errors=%d)", path, summary.ExpectedDocuments, documents, chunks, len(summary.Errors))
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %s: %w", path, err)
		}
		fileStats[path] = info
		totalDocuments += documents
		totalChunks += chunks
	}
	if totalDocuments != expectedDocuments || totalChunks != expectedChunks {
		return fmt.Errorf("corpus totals do not match expectation: got %d documents and %d chunks; want %d and %d", totalDocuments, totalChunks, expectedDocuments, expectedChunks)
	}
	if profile == nil {
		return fmt.Errorf("no collection profile found")
	}

	collectionName := strings.TrimSpace(collectionOverride)
	if collectionName == "" {
		collectionName = profile.Name
	}
	if collectionName == "" {
		return fmt.Errorf("collection name is empty")
	}
	collectionID, err := ensureCorpusCollection(ctx, pool, *profile, collectionName)
	if err != nil {
		return err
	}
	if progress != nil {
		_, _ = fmt.Fprintf(progress, "Validated %d documents and %d chunks; importing into collection %q.\n", totalDocuments, totalChunks, collectionName)
	}

	importedDocuments, importedChunks := 0, 0
	for _, path := range paths {
		before := fileStats[path]
		current, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %s before import: %w", path, err)
		}
		if current.Size() != before.Size() || !current.ModTime().Equal(before.ModTime()) {
			return fmt.Errorf("%s changed after validation; no documents from this file were imported", path)
		}

		_, _, _, _, err = scanCorpusJSON(path, func(p corpusCollection, d corpusDocument) error {
			if !sameCorpusCollection(*profile, p) {
				return fmt.Errorf("%s: collection profile changed after validation", path)
			}
			if err := validateCorpusDocument(p, d); err != nil {
				return fmt.Errorf("source_key %q changed after validation: %w", d.SourceKey, err)
			}
			chunks, err := importCorpusDocument(ctx, pool, collectionID, d)
			if err != nil {
				return fmt.Errorf("import %s: %w", d.SourceKey, err)
			}
			importedDocuments++
			importedChunks += chunks
			if progress != nil && (importedDocuments%25 == 0 || importedDocuments == totalDocuments) {
				_, _ = fmt.Fprintf(progress, "Imported %d/%d documents (%d/%d chunks).\n", importedDocuments, totalDocuments, importedChunks, totalChunks)
			}
			return nil
		})
		if err != nil {
			return err
		}
		current, err = os.Stat(path)
		if err != nil {
			return fmt.Errorf("stat %s after import: %w", path, err)
		}
		if current.Size() != before.Size() || !current.ModTime().Equal(before.ModTime()) {
			return fmt.Errorf("%s changed during import; rerun after restoring the validated file", path)
		}
	}
	if importedDocuments != expectedDocuments || importedChunks != expectedChunks {
		return fmt.Errorf("import totals differ from expectation: got %d documents and %d chunks", importedDocuments, importedChunks)
	}
	return nil
}

type corpusCollection struct {
	Name                 string          `json:"name"`
	EmbeddingModel       string          `json:"embedding_model"`
	EmbeddingRevision    *string         `json:"embedding_revision"`
	EmbeddingDimension   int             `json:"embedding_dimension"`
	EmbeddingTextVersion int             `json:"embedding_text_version"`
	QueryPromptName      string          `json:"query_prompt_name"`
	ChunkingConfig       json.RawMessage `json:"chunking_config"`
	EmbeddingsIncluded   bool            `json:"embeddings_included"`
}

type corpusSummary struct {
	ExpectedDocuments int               `json:"expected_documents"`
	Documents         int               `json:"documents"`
	Chunks            int               `json:"chunks"`
	Complete          bool              `json:"complete"`
	Errors            []json.RawMessage `json:"errors"`
}

type corpusDocument struct {
	SourceKey   string          `json:"source_key"`
	SourcePath  string          `json:"source_path"`
	Title       string          `json:"title"`
	Content     string          `json:"content"`
	SourceURL   string          `json:"source_url"`
	Origin      string          `json:"origin"`
	MinRole     string          `json:"min_role"`
	Team        *string         `json:"team"`
	Tags        []string        `json:"tags"`
	ContentHash string          `json:"content_hash"`
	Metadata    json.RawMessage `json:"metadata"`
	Chunks      []corpusChunk   `json:"chunks"`
}

type corpusChunk struct {
	Position      int       `json:"position"`
	Section       string    `json:"section"`
	Text          string    `json:"text"`
	EmbeddingText string    `json:"embedding_text"`
	TokenCount    int       `json:"token_count"`
	ContentHash   string    `json:"content_hash"`
	Embedding     []float64 `json:"embedding"`
}

func scanCorpusJSON(path string, visit func(corpusCollection, corpusDocument) error) (corpusCollection, corpusSummary, int, int, error) {
	file, err := os.Open(path)
	if err != nil {
		return corpusCollection{}, corpusSummary{}, 0, 0, fmt.Errorf("open %s: %w", path, err)
	}
	defer file.Close()
	decoder := json.NewDecoder(file)

	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return corpusCollection{}, corpusSummary{}, 0, 0, fmt.Errorf("%s: expected a JSON object", path)
	}
	var schemaVersion int
	var profile corpusCollection
	var summary corpusSummary
	var errorsSeen, profileSeen, summarySeen, documentsSeen bool
	documentCount, chunkCount := 0, 0
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: read property: %w", path, err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: invalid JSON property name", path)
		}
		switch key {
		case "schema_version":
			if err := decoder.Decode(&schemaVersion); err != nil {
				return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: decode schema_version: %w", path, err)
			}
		case "collection":
			if profileSeen {
				return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: duplicate collection property", path)
			}
			if err := decoder.Decode(&profile); err != nil {
				return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: decode collection: %w", path, err)
			}
			profileSeen = true
		case "documents":
			if documentsSeen {
				return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: duplicate documents property", path)
			}
			if !profileSeen {
				return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: collection must precede documents", path)
			}
			arrayStart, err := decoder.Token()
			if err != nil || arrayStart != json.Delim('[') {
				return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: documents must be an array", path)
			}
			for decoder.More() {
				var document corpusDocument
				if err := decoder.Decode(&document); err != nil {
					return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: decode document %d: %w", path, documentCount+1, err)
				}
				if visit != nil {
					if err := visit(profile, document); err != nil {
						return profile, summary, documentCount, chunkCount, fmt.Errorf("%s document %d: %w", path, documentCount+1, err)
					}
				}
				documentCount++
				chunkCount += len(document.Chunks)
			}
			arrayEnd, err := decoder.Token()
			if err != nil || arrayEnd != json.Delim(']') {
				return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: documents array is not closed", path)
			}
			documentsSeen = true
		case "summary":
			if summarySeen {
				return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: duplicate summary property", path)
			}
			if err := decoder.Decode(&summary); err != nil {
				return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: decode summary: %w", path, err)
			}
			summarySeen = true
		case "errors":
			if errorsSeen {
				return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: duplicate errors property", path)
			}
			if err := decoder.Decode(&summary.Errors); err != nil {
				return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: decode errors: %w", path, err)
			}
			errorsSeen = true
		default:
			var ignored json.RawMessage
			if err := decoder.Decode(&ignored); err != nil {
				return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: decode property %q: %w", path, key, err)
			}
		}
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') {
		return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: JSON object is not closed", path)
	}
	if _, err := decoder.Token(); err != io.EOF {
		return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: unexpected data after JSON object", path)
	}
	if schemaVersion != 2 || !profileSeen || !documentsSeen || !summarySeen || !errorsSeen {
		return profile, summary, documentCount, chunkCount, fmt.Errorf("%s: requires schema_version=2, collection, documents, summary and errors", path)
	}
	return profile, summary, documentCount, chunkCount, nil
}

func validateCorpusCollection(profile corpusCollection) error {
	if strings.TrimSpace(profile.Name) == "" || strings.TrimSpace(profile.EmbeddingModel) == "" {
		return fmt.Errorf("collection name and embedding_model are required")
	}
	if profile.EmbeddingDimension != Dimension || profile.EmbeddingTextVersion != 1 || profile.QueryPromptName != "query" {
		return fmt.Errorf("unsupported collection dimension, embedding_text_version or query_prompt_name")
	}
	if !profile.EmbeddingsIncluded {
		return fmt.Errorf("export contains no embeddings")
	}
	var config map[string]json.RawMessage
	if err := json.Unmarshal(profile.ChunkingConfig, &config); err != nil || config == nil {
		return fmt.Errorf("chunking_config must be a JSON object")
	}
	return nil
}

func sameCorpusCollection(a, b corpusCollection) bool {
	if a.Name != b.Name || a.EmbeddingModel != b.EmbeddingModel || a.EmbeddingDimension != b.EmbeddingDimension || a.EmbeddingTextVersion != b.EmbeddingTextVersion || a.QueryPromptName != b.QueryPromptName || a.EmbeddingsIncluded != b.EmbeddingsIncluded {
		return false
	}
	if (a.EmbeddingRevision == nil) != (b.EmbeddingRevision == nil) {
		return false
	}
	if a.EmbeddingRevision != nil && *a.EmbeddingRevision != *b.EmbeddingRevision {
		return false
	}
	var aConfig, bConfig any
	if json.Unmarshal(a.ChunkingConfig, &aConfig) != nil || json.Unmarshal(b.ChunkingConfig, &bConfig) != nil {
		return false
	}
	return reflect.DeepEqual(aConfig, bConfig)
}

func validateCorpusDocument(profile corpusCollection, document corpusDocument) error {
	if strings.TrimSpace(document.SourceKey) == "" || strings.TrimSpace(document.Title) == "" || strings.TrimSpace(document.Content) == "" || strings.TrimSpace(document.SourceURL) == "" {
		return fmt.Errorf("source_key, title, content and source_url are required")
	}
	if document.Origin != "public" && document.Origin != "synthetic" && document.Origin != "provided" {
		return fmt.Errorf("invalid origin %q", document.Origin)
	}
	if document.MinRole != "intern" && document.MinRole != "buddy" && document.MinRole != "manager" {
		return fmt.Errorf("invalid min_role %q", document.MinRole)
	}
	if document.Team != nil && strings.TrimSpace(*document.Team) == "" {
		return fmt.Errorf("team must be null or a nonblank string")
	}
	if document.ContentHash != hashText(canonicalDocument(document.Title, document.Content)) {
		return fmt.Errorf("document content_hash mismatch")
	}
	if len(document.Chunks) == 0 {
		return fmt.Errorf("document contains no chunks")
	}
	metadata := document.Metadata
	if len(metadata) == 0 || string(metadata) == "null" {
		metadata = json.RawMessage(`{}`)
	}
	var metadataObject map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &metadataObject); err != nil || metadataObject == nil {
		return fmt.Errorf("metadata must be a JSON object")
	}
	for i, chunk := range document.Chunks {
		position := i + 1
		if chunk.Position != position {
			return fmt.Errorf("chunk positions must be contiguous starting at 1 (got %d at %d)", chunk.Position, position)
		}
		if strings.TrimSpace(chunk.Text) == "" || chunk.TokenCount <= 0 {
			return fmt.Errorf("chunk %d has empty text or invalid token_count", position)
		}
		wantText := document.Title + "\n" + chunk.Text
		if strings.TrimSpace(chunk.Section) != "" {
			wantText = document.Title + " | " + chunk.Section + "\n" + chunk.Text
		}
		if chunk.EmbeddingText != wantText {
			return fmt.Errorf("chunk %d embedding_text does not match title, section and text", position)
		}
		if chunk.ContentHash != hashText(chunk.EmbeddingText) {
			return fmt.Errorf("chunk %d content_hash mismatch", position)
		}
		if len(chunk.Embedding) != profile.EmbeddingDimension {
			return fmt.Errorf("chunk %d has vector dimension %d, expected %d", position, len(chunk.Embedding), profile.EmbeddingDimension)
		}
		norm := 0.0
		for _, value := range chunk.Embedding {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return fmt.Errorf("chunk %d contains a non-finite vector value", position)
			}
			norm = math.Hypot(norm, value)
		}
		if norm < 0.999 || norm > 1.001 {
			return fmt.Errorf("chunk %d vector norm %.8f is outside [0.999, 1.001]", position, norm)
		}
	}
	return nil
}

func ensureCorpusCollection(ctx context.Context, pool *pgxpool.Pool, profile corpusCollection, name string) (int64, error) {
	var revision any
	if profile.EmbeddingRevision != nil {
		revision = *profile.EmbeddingRevision
	}
	_, err := pool.Exec(ctx, `INSERT INTO rag.collections
		(name,embedding_model,embedding_revision,embedding_dimension,embedding_text_version,query_prompt_name,chunking_config)
		VALUES ($1,$2,$3,$4,$5,$6,$7::jsonb) ON CONFLICT (name) DO NOTHING`,
		name, profile.EmbeddingModel, revision, profile.EmbeddingDimension, profile.EmbeddingTextVersion, profile.QueryPromptName, string(profile.ChunkingConfig))
	if err != nil {
		return 0, fmt.Errorf("create collection (apply migrations first): %w", err)
	}
	var id int64
	err = pool.QueryRow(ctx, `SELECT id FROM rag.collections
		WHERE name=$1 AND embedding_model=$2 AND embedding_revision IS NOT DISTINCT FROM $3
		AND embedding_dimension=$4 AND embedding_text_version=$5 AND query_prompt_name=$6
		AND chunking_config=$7::jsonb`,
		name, profile.EmbeddingModel, revision, profile.EmbeddingDimension, profile.EmbeddingTextVersion, profile.QueryPromptName, string(profile.ChunkingConfig)).Scan(&id)
	if err == pgx.ErrNoRows {
		return 0, fmt.Errorf("collection %q already exists with a different embedding profile; choose a new collection name", name)
	}
	if err != nil {
		return 0, fmt.Errorf("read collection profile: %w", err)
	}
	return id, nil
}

func importCorpusDocument(ctx context.Context, pool *pgxpool.Pool, collectionID int64, document corpusDocument) (int, error) {
	metadata := document.Metadata
	if len(metadata) == 0 || string(metadata) == "null" {
		metadata = json.RawMessage(`{}`)
	}
	tags := document.Tags
	if tags == nil {
		tags = []string{}
	}
	var team any
	if document.Team != nil {
		team = *document.Team
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	var id int64
	err = tx.QueryRow(ctx, `INSERT INTO rag.documents
		(collection_id,source_key,source_path,title,content,source_url,origin,min_role,team,tags,content_hash,metadata)
		VALUES ($1,$2,NULLIF($3,''),$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb)
		ON CONFLICT (collection_id,source_key) DO UPDATE SET
		source_path=EXCLUDED.source_path,title=EXCLUDED.title,content=EXCLUDED.content,
		source_url=EXCLUDED.source_url,origin=EXCLUDED.origin,min_role=EXCLUDED.min_role,
		team=EXCLUDED.team,tags=EXCLUDED.tags,content_hash=EXCLUDED.content_hash,
		metadata=EXCLUDED.metadata,is_active=true,updated_at=now()
		RETURNING id`, collectionID, document.SourceKey, document.SourcePath, document.Title,
		document.Content, document.SourceURL, document.Origin, document.MinRole, team, tags,
		document.ContentHash, string(metadata)).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upsert document: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM rag.chunks WHERE doc_id=$1`, id); err != nil {
		return 0, fmt.Errorf("replace old chunks: %w", err)
	}
	for _, chunk := range document.Chunks {
		_, err := tx.Exec(ctx, `INSERT INTO rag.chunks
			(doc_id,position,section,text,embedding_text,token_count,content_hash,embedding)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8::text::public.vector(1024))`,
			id, chunk.Position, chunk.Section, chunk.Text, chunk.EmbeddingText, chunk.TokenCount,
			chunk.ContentHash, corpusVectorLiteral(chunk.Embedding))
		if err != nil {
			return 0, fmt.Errorf("insert chunk %d: %w", chunk.Position, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit document: %w", err)
	}
	return len(document.Chunks), nil
}

func corpusVectorLiteral(values []float64) string {
	var b strings.Builder
	b.Grow(len(values) * 12)
	b.WriteByte('[')
	for i, value := range values {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(value, 'g', -1, 64))
	}
	b.WriteByte(']')
	return b.String()
}
