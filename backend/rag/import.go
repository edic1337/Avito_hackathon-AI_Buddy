package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
)

// ImportJSON reads schema_version=1 from RAG. Anonymous fields only decode the
// existing file format; there is no public Chunk model.
func (s *Store) ImportJSON(ctx context.Context, reader io.Reader, sourceKey string) (int64, error) {
	d, texts, sections, counts, vectors, err := readExport(reader, sourceKey, s.config)
	if err != nil {
		return 0, err
	}
	return s.save(ctx, d, texts, sections, counts, vectors)
}

func readExport(reader io.Reader, sourceKey string, config Config) (Document, []string, []string, []*int, [][]float32, error) {
	var payload struct {
		SchemaVersion      int             `json:"schema_version"`
		Document           Document        `json:"document"`
		EmbeddingModel     string          `json:"embedding_model"`
		EmbeddingRevision  string          `json:"embedding_revision"`
		EmbeddingDimension int             `json:"embedding_dimension"`
		Chunking           json.RawMessage `json:"chunking"`
		Chunks             []struct {
			Index      int       `json:"index"`
			Section    string    `json:"section"`
			Text       string    `json:"text"`
			TokenCount int       `json:"token_count"`
			Embedding  []float32 `json:"embedding"`
		} `json:"chunks"`
	}
	fail := func(err error) (Document, []string, []string, []*int, [][]float32, error) {
		return Document{}, nil, nil, nil, nil, err
	}
	const maxExport = 64 << 20
	raw, err := io.ReadAll(io.LimitReader(reader, maxExport+1))
	if err != nil {
		return fail(err)
	}
	if len(raw) > maxExport {
		return fail(fmt.Errorf("export exceeds 64 MiB"))
	}
	if err = json.Unmarshal(raw, &payload); err != nil {
		return fail(fmt.Errorf("decode export: %w", err))
	}
	if payload.SchemaVersion != 1 || payload.EmbeddingModel != config.EmbeddingModel || payload.EmbeddingRevision != config.EmbeddingRevision || payload.EmbeddingDimension != Dimension {
		return fail(fmt.Errorf("export schema/model/revision/dimension mismatch"))
	}
	var have, want map[string]any
	if err = json.Unmarshal(payload.Chunking, &have); err != nil {
		return fail(fmt.Errorf("invalid export chunking: %w", err))
	}
	if err = json.Unmarshal(config.ChunkingConfig, &want); err != nil {
		return fail(err)
	}
	if have == nil || !reflect.DeepEqual(have, want) {
		return fail(fmt.Errorf("export chunking profile mismatch"))
	}
	payload.Document.SourceKey = sourceKey
	if err = payload.Document.validate(); err != nil {
		return fail(err)
	}
	if len(payload.Chunks) == 0 {
		return fail(fmt.Errorf("export contains no embedded texts"))
	}
	texts := make([]string, len(payload.Chunks))
	sections := make([]string, len(texts))
	counts := make([]*int, len(texts))
	vectors := make([][]float32, len(texts))
	for i, item := range payload.Chunks {
		if item.Index != i+1 || item.TokenCount <= 0 {
			return fail(fmt.Errorf("invalid position/token count at %d", i+1))
		}
		v, err := normalize(item.Embedding)
		if err != nil {
			return fail(fmt.Errorf("export position %d: %w", i+1, err))
		}
		texts[i] = item.Text
		sections[i] = item.Section
		count := item.TokenCount
		counts[i] = &count
		vectors[i] = v
	}
	return payload.Document, texts, sections, counts, vectors, nil
}
