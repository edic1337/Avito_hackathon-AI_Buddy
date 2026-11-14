package rag

import (
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const Dimension = 1024
const DefaultModel = "Qwen/Qwen3-Embedding-0.6B"

// Exact sentence-transformers prompt_name="query" for the default Qwen model.
const queryPrompt = "Instruct: Given a web search query, retrieve relevant passages that answer the query\nQuery:"

type Config struct {
	DatabaseURL       string
	Collection        string
	EmbeddingURL      string // Full OpenAI-compatible /v1/embeddings endpoint.
	EmbeddingAPIKey   string
	EmbeddingModel    string
	EmbeddingRevision string
	ChunkingConfig    json.RawMessage
	BatchSize         int
	TopK              int
	CandidateLimit    int
	MinSimilarity     float64
	MaxContextRunes   int
	HTTPTimeout       time.Duration
}

func DefaultConfig() Config {
	return Config{
		DatabaseURL: "postgresql://postgres:postgres@localhost:5432/avito_buddy",
		Collection:  "main", EmbeddingURL: "http://localhost:8081/v1/embeddings",
		EmbeddingModel: DefaultModel,
		ChunkingConfig: json.RawMessage(`{"tokenizer_model":"Qwen/Qwen3-Embedding-0.6B","target_tokens":300,"max_tokens":400,"overlap_tokens":40}`),
		BatchSize:      8, TopK: 6, CandidateLimit: 30, MinSimilarity: 0.45,
		MaxContextRunes: 12000, HTTPTimeout: 60 * time.Second,
	}
}

// ConfigFromEnv reads the process environment; loading .env is the caller's job.
func ConfigFromEnv() (Config, error) {
	c := DefaultConfig()
	for key, dst := range map[string]*string{
		"DATABASE_URL": &c.DatabaseURL, "RAG_COLLECTION": &c.Collection,
		"EMBEDDING_API_URL": &c.EmbeddingURL, "EMBEDDING_API_KEY": &c.EmbeddingAPIKey,
		"EMBEDDING_MODEL": &c.EmbeddingModel, "EMBEDDING_REVISION": &c.EmbeddingRevision,
	} {
		if value, ok := os.LookupEnv(key); ok {
			*dst = value
		}
	}
	for key, dst := range map[string]*int{
		"EMBEDDING_BATCH_SIZE": &c.BatchSize, "TOP_K": &c.TopK,
		"RAG_CANDIDATE_LIMIT": &c.CandidateLimit, "RAG_MAX_CONTEXT_RUNES": &c.MaxContextRunes,
	} {
		if value, ok := os.LookupEnv(key); ok {
			n, err := strconv.Atoi(value)
			if err != nil {
				return c, fmt.Errorf("invalid %s: %w", key, err)
			}
			*dst = n
		}
	}
	if value, ok := os.LookupEnv("MIN_SIMILARITY"); ok {
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return c, fmt.Errorf("invalid MIN_SIMILARITY: %w", err)
		}
		c.MinSimilarity = n
	}
	if value, ok := os.LookupEnv("EMBEDDING_TIMEOUT"); ok {
		d, err := time.ParseDuration(value)
		if err != nil {
			return c, fmt.Errorf("invalid EMBEDDING_TIMEOUT: %w", err)
		}
		c.HTTPTimeout = d
	}
	if value, ok := os.LookupEnv("RAG_CHUNKING_CONFIG"); ok {
		c.ChunkingConfig = json.RawMessage(value)
	}
	return c, c.Validate()
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.DatabaseURL) == "" || strings.TrimSpace(c.Collection) == "" || strings.TrimSpace(c.EmbeddingModel) == "" {
		return fmt.Errorf("database URL, collection and embedding model are required")
	}
	u, err := url.Parse(c.EmbeddingURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil {
		return fmt.Errorf("EMBEDDING_API_URL must be an http(s) endpoint without credentials")
	}
	if c.BatchSize < 1 || c.BatchSize > 256 || c.TopK < 1 || c.TopK > 100 || c.CandidateLimit < c.TopK || c.CandidateLimit > 1000 || c.MaxContextRunes < 1 || c.HTTPTimeout <= 0 {
		return fmt.Errorf("invalid batch, retrieval limits or timeout")
	}
	if math.IsNaN(c.MinSimilarity) || math.IsInf(c.MinSimilarity, 0) || c.MinSimilarity < -1 || c.MinSimilarity > 1 {
		return fmt.Errorf("MIN_SIMILARITY must be finite and between -1 and 1")
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(c.ChunkingConfig, &obj); err != nil || obj == nil {
		return fmt.Errorf("RAG_CHUNKING_CONFIG must be a JSON object")
	}
	return nil
}

// Access must come from trusted backend authentication, never request JSON.
type Access struct {
	Role string
	Team string
}

func roleRank(role string) (int, error) {
	switch role {
	case "intern":
		return 1, nil
	case "buddy":
		return 2, nil
	case "manager":
		return 3, nil
	default:
		return 0, fmt.Errorf("unknown role %q", role)
	}
}
