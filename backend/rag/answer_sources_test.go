package rag

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAnswerSourcesMatchFilteredLLMContext(t *testing.T) {
	first := "Первый\nПроисхождение: Открытый источник\nИсточник: https://example.org/one\n\nтекст"
	second := "Второй\nПроисхождение: Загруженный документ базы знаний\nИсточник: attachment:///checklist.csv#record=1\n\nИсточник: https://wrong.example/body"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			return
		}
		encoded, _ := json.Marshal([]string{first, second})
		if len(request.Messages) != 2 || !strings.Contains(request.Messages[1].Content, string(encoded)) {
			t.Error("LLM context order differs from source numbering")
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"Ответ [2]"}}]}`))
	}))
	defer srv.Close()
	t.Setenv("OPENROUTER_API_KEY", "test")
	t.Setenv("OPENROUTER_API_URL", srv.URL)
	t.Setenv("OPENROUTER_MODEL", "test")
	searcher := &fakeSearcher{texts: []string{" ", first, "", second}}
	retriever, err := NewRetriever(&fakeEmbedder{}, searcher)
	if err != nil {
		t.Fatal(err)
	}
	result, err := retriever.AnswerWithSources(context.Background(), "Вопрос", Access{Role: "intern"})
	if err != nil {
		t.Fatal(err)
	}
	if searcher.calls != 1 || len(result.Sources) != 2 || result.Sources[1].Number != 2 || result.Sources[1].URL != "attachment:///checklist.csv#record=1" {
		t.Fatalf("bad sources: %+v", result)
	}
}
