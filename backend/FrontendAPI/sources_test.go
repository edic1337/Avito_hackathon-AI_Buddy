package FrontendAPI

import (
	"GO/rag"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

type sourceStub struct{ calls int }

func (s *sourceStub) Answer(context.Context, string, rag.Access) (string, error) {
	panic("legacy method should not be called")
}
func (s *sourceStub) AnswerWithSources(_ context.Context, _ string, access rag.Access) (rag.AnswerResult, error) {
	s.calls++
	return rag.AnswerResult{Answer: access.Role, Sources: []rag.Source{{Number: 1, Title: "Документ", URL: "https://example.org"}}}, nil
}
func TestQuestionReturnsSourcesAfterAuthentication(t *testing.T) {
	stub := &sourceStub{}
	h, err := NewQuestionHandler(stub, testAuth(t))
	if err != nil {
		t.Fatal(err)
	}
	r := request(`{"question":"Вопрос"}`)
	r.Header.Del("Authorization")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 || stub.calls != 0 {
		t.Fatal("unauthorized request reached sources")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, request(`{"question":"Вопрос"}`))
	var result QuestionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if w.Code != 200 || stub.calls != 1 || result.Answer != "intern" || len(result.Sources) != 1 {
		t.Fatalf("bad response: %+v", result)
	}
}
