package rag

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func unitVector(index int) []float32 { v := make([]float32, Dimension); v[index] = 1; return v }

func TestHTTPEmbedderContract(t *testing.T) {
	var batches [][]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-key" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("invalid HTTP contract")
		}
		var request struct {
			Model  string   `json:"model"`
			Input  []string `json:"input"`
			Format string   `json:"encoding_format"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		batches = append(batches, request.Input)
		if request.Model != DefaultModel || request.Format != "float" {
			t.Error("invalid model/format")
		}
		data := []any{}
		for i := len(request.Input) - 1; i >= 0; i-- {
			v := unitVector(i)
			v[i] = 3
			data = append(data, map[string]any{"index": i, "embedding": v})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()
	c := DefaultConfig()
	c.EmbeddingURL = srv.URL
	c.EmbeddingAPIKey = "test-key"
	c.BatchSize = 2
	e, err := NewHTTPEmbedder(c)
	if err != nil {
		t.Fatal(err)
	}
	vectors, err := e.EmbedDocuments(context.Background(), []string{"один", "два", "три"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vectors) != 3 || vectors[0][0] != 1 || vectors[1][1] != 1 || vectors[2][0] != 1 {
		t.Fatal("response indices/normalization lost")
	}
	if _, err = e.EmbedQuery(context.Background(), "VPN?"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(batches, [][]string{{"один", "два"}, {"три"}, {queryPrompt + "VPN?"}}) {
		t.Fatalf("unexpected input: %v", batches)
	}
	if v, err := e.EmbedDocuments(context.Background(), nil); err != nil || len(v) != 0 {
		t.Fatal(v, err)
	}
	if _, err = e.EmbedDocuments(context.Background(), []string{"ok", " "}); err == nil {
		t.Fatal("accepted empty text")
	}
	if _, err = e.EmbedQuery(context.Background(), " "); err == nil {
		t.Fatal("accepted empty query")
	}
}

func TestHTTPEmbedderFailures(t *testing.T) {
	valid := unitVector(0)
	for _, tc := range []struct {
		name   string
		status int
		body   any
	}{
		{"status", 503, map[string]any{"error": "private server details"}},
		{"invalid-json", 200, "not json"},
		{"count", 200, map[string]any{"data": []any{}}},
		{"missing-index", 200, map[string]any{"data": []any{map[string]any{"embedding": valid}}}},
		{"bad-index", 200, map[string]any{"data": []any{map[string]any{"index": 2, "embedding": valid}}}},
		{"dimension", 200, map[string]any{"data": []any{map[string]any{"index": 0, "embedding": []int{1}}}}},
		{"zero", 200, map[string]any{"data": []any{map[string]any{"index": 0, "embedding": make([]float32, Dimension)}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				if s, ok := tc.body.(string); ok {
					_, _ = w.Write([]byte(s))
				} else {
					_ = json.NewEncoder(w).Encode(tc.body)
				}
			}))
			defer srv.Close()
			c := DefaultConfig()
			c.EmbeddingURL = srv.URL
			e, _ := NewHTTPEmbedder(c)
			if _, err := e.EmbedQuery(context.Background(), "test"); err == nil || strings.Contains(err.Error(), "private server") {
				t.Fatalf("expected sanitized error, got %v", err)
			}
		})
	}
	t.Run("duplicate", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"index": 0, "embedding": valid}, map[string]any{"index": 0, "embedding": valid}}})
		}))
		defer srv.Close()
		c := DefaultConfig()
		c.EmbeddingURL = srv.URL
		e, _ := NewHTTPEmbedder(c)
		if _, err := e.EmbedDocuments(context.Background(), []string{"a", "b"}); err == nil {
			t.Fatal("duplicate accepted")
		}
	})
	t.Run("cancel-and-timeout", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(200 * time.Millisecond):
			}
		}))
		defer srv.Close()
		c := DefaultConfig()
		c.EmbeddingURL = srv.URL
		c.HTTPTimeout = 20 * time.Millisecond
		e, _ := NewHTTPEmbedder(c)
		if _, err := e.EmbedQuery(context.Background(), "test"); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("expected timeout: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := e.EmbedQuery(ctx, "test"); !errors.Is(err, context.Canceled) {
			t.Fatalf("expected cancellation: %v", err)
		}
	})
}

func TestNormalize(t *testing.T) {
	for _, v := range [][]float32{nil, {1}, make([]float32, Dimension)} {
		if _, err := normalize(v); err == nil {
			t.Fatal("invalid vector accepted")
		}
	}
	for _, value := range []float32{float32(math.NaN()), float32(math.Inf(1))} {
		v := unitVector(0)
		v[0] = value
		if _, err := normalize(v); err == nil {
			t.Fatal("non-finite accepted")
		}
	}
	v := unitVector(0)
	v[0] = math.SmallestNonzeroFloat32
	n, err := normalize(v)
	if err != nil || n[0] != 1 || v[0] == 1 {
		t.Fatal("normalization mutated input or underflowed")
	}
	if vectorLiteral([]float32{1, -0.5}) != "[1,-0.5]" {
		t.Fatal("invalid vector serialization")
	}
}
