package rag

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
)

type Embedder interface {
	EmbedQuery(context.Context, string) ([]float32, error)
	EmbedDocuments(context.Context, []string) ([][]float32, error)
}

type HTTPEmbedder struct {
	config Config
	client *http.Client
}

func NewHTTPEmbedder(config Config) (*HTTPEmbedder, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	return &HTTPEmbedder{config: config, client: &http.Client{Timeout: config.HTTPTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

func (e *HTTPEmbedder) EmbedQuery(ctx context.Context, question string) ([]float32, error) {
	if strings.TrimSpace(question) == "" {
		return nil, fmt.Errorf("question must not be empty")
	}
	vectors, err := e.encode(ctx, []string{queryPrompt + question})
	if err != nil {
		return nil, err
	}
	return vectors[0], nil
}

func (e *HTTPEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	for _, text := range texts {
		if strings.TrimSpace(text) == "" {
			return nil, fmt.Errorf("embedding text must not be empty")
		}
	}
	result := make([][]float32, 0, len(texts))
	for start := 0; start < len(texts); start += e.config.BatchSize {
		end := min(start+e.config.BatchSize, len(texts))
		vectors, err := e.encode(ctx, texts[start:end])
		if err != nil {
			return nil, err
		}
		result = append(result, vectors...)
	}
	return result, nil
}

func (e *HTTPEmbedder) encode(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(struct {
		Model          string   `json:"model"`
		Input          []string `json:"input"`
		EncodingFormat string   `json:"encoding_format"`
	}{e.config.EmbeddingModel, texts, "float"})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.config.EmbeddingURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.config.EmbeddingAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+e.config.EmbeddingAPIKey)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("embedding endpoint returned HTTP %d", resp.StatusCode)
	}
	const maxResponse = 32 << 20
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, fmt.Errorf("read embeddings: %w", err)
	}
	if len(raw) > maxResponse {
		return nil, fmt.Errorf("embedding response exceeds 32 MiB")
	}
	var payload struct {
		Data []struct {
			Index     *int      `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode embeddings: %w", err)
	}
	if len(payload.Data) != len(texts) {
		return nil, fmt.Errorf("embedding count mismatch: got %d, want %d", len(payload.Data), len(texts))
	}
	result := make([][]float32, len(texts))
	for _, item := range payload.Data {
		if item.Index == nil || *item.Index < 0 || *item.Index >= len(result) || result[*item.Index] != nil {
			return nil, fmt.Errorf("invalid or duplicate embedding index")
		}
		v, err := normalize(item.Embedding)
		if err != nil {
			return nil, err
		}
		result[*item.Index] = v
	}
	return result, nil
}

func normalize(vector []float32) ([]float32, error) {
	if len(vector) != Dimension {
		return nil, fmt.Errorf("embedding dimension: got %d, want %d", len(vector), Dimension)
	}
	var norm float64
	for _, v := range vector {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, fmt.Errorf("embedding contains non-finite values")
		}
		norm = math.Hypot(norm, float64(v))
	}
	if norm == 0 {
		return nil, fmt.Errorf("embedding must not be zero")
	}
	result := make([]float32, len(vector))
	for i, v := range vector {
		result[i] = float32(float64(v) / norm)
	}
	return result, nil
}

// Values are bound as text and cast by SQL; no dynamic SQL or vector codec needed.
func vectorLiteral(vector []float32) string {
	var b strings.Builder
	b.WriteByte('[')
	for i, v := range vector {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(strconv.FormatFloat(float64(v), 'g', -1, 32))
	}
	b.WriteByte(']')
	return b.String()
}
