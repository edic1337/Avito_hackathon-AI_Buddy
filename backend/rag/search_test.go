package rag

import (
	"context"
	"errors"
	"testing"

	"GO/ModelAPI"
)

type fakeEmbedder struct {
	err   error
	calls int
	texts []string
}

func (f *fakeEmbedder) EmbedQuery(ctx context.Context, q string) ([]float32, error) {
	f.calls++
	return unitVector(0), f.err
}
func (f *fakeEmbedder) EmbedDocuments(ctx context.Context, texts []string) ([][]float32, error) {
	f.calls++
	f.texts = texts
	v := make([][]float32, len(texts))
	for i := range v {
		v[i] = unitVector(0)
	}
	return v, f.err
}

type fakeSearcher struct {
	err    error
	texts  []string
	calls  int
	access Access
}

func (f *fakeSearcher) Search(ctx context.Context, q string, v []float32, a Access) ([]string, error) {
	f.calls++
	f.access = a
	return f.texts, f.err
}

func TestRetrieverAndAnswer(t *testing.T) {
	e := &fakeEmbedder{}
	s := &fakeSearcher{texts: []string{"документ"}}
	r, err := NewRetriever(e, s)
	if err != nil {
		t.Fatal(err)
	}
	a := Access{"intern", "data"}
	texts, err := r.Retrieve(context.Background(), "VPN?", a)
	if err != nil || len(texts) != 1 || s.access != a {
		t.Fatal(texts, err)
	}
	if _, err = r.Retrieve(context.Background(), "VPN?", Access{"admin", ""}); err == nil {
		t.Fatal("unknown role accepted")
	}
	if _, err = r.Retrieve(context.Background(), " ", a); err == nil {
		t.Fatal("empty question accepted")
	}
	if e.calls != 1 {
		t.Fatal("invalid input called embedder")
	}
	e.err = errors.New("embed failure")
	if _, err = r.Retrieve(context.Background(), "VPN?", a); !errors.Is(err, e.err) {
		t.Fatal(err)
	}
	e.err = nil
	s.err = errors.New("db failure")
	if _, err = r.Answer(context.Background(), "VPN?", a); !errors.Is(err, s.err) {
		t.Fatal(err)
	}
	s.err = nil
	s.texts = nil
	t.Setenv("OPENROUTER_API_KEY", "")
	answer, err := r.Answer(context.Background(), "VPN?", a)
	if err != nil || answer != ModelAPI.NoContextAnswer {
		t.Fatal(answer, err)
	}
	if _, err = NewRetriever(nil, s); err == nil {
		t.Fatal("nil embedder")
	}
	if _, err = NewRetriever(e, nil); err == nil {
		t.Fatal("nil store")
	}
}
