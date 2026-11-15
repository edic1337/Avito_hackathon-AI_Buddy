package rag

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestConfig(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	for key, value := range map[string]string{"TOP_K": "bad", "MIN_SIMILARITY": "NaN", "EMBEDDING_TIMEOUT": "bad", "RAG_CHUNKING_CONFIG": "[]", "EMBEDDING_API_URL": "file:///tmp", "EMBEDDING_BATCH_SIZE": "0"} {
		t.Run(key, func(t *testing.T) {
			t.Setenv(key, value)
			if _, err := ConfigFromEnv(); err == nil {
				t.Fatal("bad config accepted")
			}
		})
	}
	t.Setenv("TOP_K", "2")
	t.Setenv("MIN_SIMILARITY", "0.6")
	t.Setenv("EMBEDDING_TIMEOUT", "3s")
	t.Setenv("RAG_COLLECTION", "demo")
	c, err := ConfigFromEnv()
	if err != nil || c.TopK != 2 || c.MinSimilarity != 0.6 || c.Collection != "demo" {
		t.Fatal(c, err)
	}
	c.EmbeddingURL = "http://user:pass@localhost"
	if _, err = NewHTTPEmbedder(c); err == nil {
		t.Fatal("URL credentials accepted")
	}
	c = DefaultConfig()
	c.DatabaseURL = ""
	if err = c.Validate(); err == nil {
		t.Fatal("empty DB")
	}
	c = DefaultConfig()
	c.MinSimilarity = 2
	if err = c.Validate(); err == nil {
		t.Fatal("bad cosine")
	}
}

func sampleExport(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"schema_version": 1, "document": map[string]any{"title": "Доступ", "text": "VPN", "source_url": "demo://vpn", "origin": "synthetic", "min_role": "intern", "team": nil, "tags": []string{}},
		"embedding_model": DefaultModel, "embedding_dimension": Dimension, "chunking": json.RawMessage(DefaultConfig().ChunkingConfig),
		"chunks": []any{map[string]any{"index": 1, "section": "VPN", "text": "Запросите доступ", "token_count": 12, "embedding": unitVector(0)}},
	}
}

func TestReadExport(t *testing.T) {
	p := sampleExport(t)
	raw, _ := json.Marshal(p)
	d, texts, sections, counts, vectors, err := readExport(strings.NewReader(string(raw)), "vpn", DefaultConfig())
	if err != nil || d.SourceKey != "vpn" || d.Team != "" || d.Origin != "synthetic" || len(texts) != 1 || sections[0] != "VPN" || *counts[0] != 12 || vectors[0][0] != 1 {
		t.Fatal(d, texts, err)
	}
	for _, tc := range []struct {
		key   string
		value any
	}{
		{"schema_version", 2}, {"embedding_model", "other"}, {"embedding_dimension", 12}, {"embedding_revision", "different"}, {"chunking", map[string]any{}}, {"chunks", []any{}},
		{"chunks", []any{map[string]any{"index": 2, "token_count": 12, "embedding": unitVector(0)}}},
		{"chunks", []any{map[string]any{"index": 1, "token_count": 12, "embedding": []int{1}}}},
	} {
		t.Run(tc.key, func(t *testing.T) {
			p := sampleExport(t)
			p[tc.key] = tc.value
			raw, _ := json.Marshal(p)
			if _, _, _, _, _, err := readExport(strings.NewReader(string(raw)), "vpn", DefaultConfig()); err == nil {
				t.Fatal("invalid export accepted")
			}
		})
	}
	if _, _, _, _, _, err := readExport(strings.NewReader("{} trailing"), "vpn", DefaultConfig()); err == nil {
		t.Fatal("invalid JSON accepted")
	}
	if _, _, _, _, _, err := readExport(strings.NewReader(string(raw)), "", DefaultConfig()); err == nil {
		t.Fatal("missing key accepted")
	}
}

func TestValidationBeforeDatabase(t *testing.T) {
	d := Document{SourceKey: "vpn", Title: "VPN", SourceURL: "demo://vpn", Origin: "synthetic", MinRole: "intern"}
	s := &Store{}
	e := &fakeEmbedder{}
	if _, err := s.SaveDocument(context.Background(), d, []string{" "}, e); err == nil || e.calls != 0 {
		t.Fatal("empty text reached embedding")
	}
	if _, err := s.SaveDocument(context.Background(), d, nil, e); err == nil {
		t.Fatal("empty document")
	}
	if _, err := s.save(context.Background(), d, []string{"a"}, nil, nil, nil); err == nil {
		t.Fatal("mismatched arrays")
	}
	if _, err := s.Search(context.Background(), "q", unitVector(0), Access{"unknown", ""}); err == nil {
		t.Fatal("bad role reached db")
	}
	if _, err := s.Search(context.Background(), "", unitVector(0), Access{"intern", ""}); err == nil {
		t.Fatal("empty query reached db")
	}
	if _, err := s.Search(context.Background(), "q", nil, Access{"intern", ""}); err == nil {
		t.Fatal("bad vector reached db")
	}
	for _, change := range []func(*Document){func(d *Document) { d.SourceKey = "" }, func(d *Document) { d.MinRole = "admin" }, func(d *Document) { d.Origin = "private" }, func(d *Document) { d.Team = " " }} {
		bad := d
		change(&bad)
		if err := bad.validate(); err == nil {
			t.Fatal("invalid metadata accepted")
		}
	}
}

func TestCanonicalDocument(t *testing.T) {
	got := canonicalDocument("Привет <>&\u2028", "\\u2028\n\t\r\b\f\x01\"\u2029")
	want := "[\"Привет <>&\u2028\",\"\\\\u2028\\n\\t\\r\\b\\f\\u0001\\\"\u2029\"]"
	if got != want {
		t.Fatalf("canonical JSON mismatch\ngot %q\nwant %q", got, want)
	}
}
