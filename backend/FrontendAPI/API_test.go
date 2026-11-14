package FrontendAPI

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"GO/ModelAPI"
	"GO/rag"
)

const testToken = "0123456789abcdef0123456789abcdef"

type answerFunc func(context.Context, string, rag.Access) (string, error)

func (f answerFunc) Answer(ctx context.Context, q string, a rag.Access) (string, error) {
	return f(ctx, q, a)
}

func testAuth(t *testing.T) Authenticator {
	t.Helper()
	auth, err := NewTokenAuthenticator(map[string]rag.Access{testToken: {Role: "intern", Team: "backend"}})
	if err != nil {
		t.Fatal(err)
	}
	return auth
}

func testHandler(t *testing.T, f answerFunc) *QuestionHandler {
	t.Helper()
	h, err := NewQuestionHandler(f, testAuth(t))
	if err != nil {
		t.Fatal(err)
	}
	h.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return h
}

func request(body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/question", strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	r.Header.Set("Authorization", "Bearer "+testToken)
	return r
}

func TestQuestionUsesServerIdentity(t *testing.T) {
	called := false
	h := testHandler(t, func(ctx context.Context, q string, access rag.Access) (string, error) {
		called = true
		if q != "Как получить VPN?" || access != (rag.Access{Role: "intern", Team: "backend"}) {
			t.Fatalf("wrong input: %q %+v", q, access)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("missing deadline")
		}
		return "Обратитесь к наставнику [1]", nil
	})
	r := request(`{"question":"  Как получить VPN?  "}`)
	r.Header.Set("X-Role", "manager")
	r.Header.Set("X-Team", "secret")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !called {
		t.Fatal(w.Code, w.Body.String())
	}
	var response QuestionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Question != "Как получить VPN?" || response.Answer != "Обратитесь к наставнику [1]" || response.Error != "" {
		t.Fatal(response)
	}
	if w.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatal("wrong response type")
	}
}

func TestQuestionRejectsInvalidRequestsBeforeRAG(t *testing.T) {
	h := testHandler(t, func(context.Context, string, rag.Access) (string, error) {
		t.Fatal("invalid request reached RAG")
		return "", nil
	})
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"empty", "", 400}, {"malformed", "{", 400}, {"null", "null", 400},
		{"missing-question", `{}`, 400}, {"blank-question", `{"question":"  "}`, 400},
		{"wrong-type", `{"question":42}`, 400},
		{"role-in-body", `{"question":"q","access":"manager"}`, 400},
		{"team-in-body", `{"question":"q","team":"secret"}`, 400},
		{"two-objects", `{"question":"q"}{}`, 400},
		{"trailing-null", `{"question":"q"}null`, 400},
		{"trailing-garbage", `{"question":"q"}bad`, 400},
		{"oversized", `{"question":"` + strings.Repeat("a", maxRequestBytes) + `"}`, 413},
		{"oversized-trailing-space", `{"question":"q"}` + strings.Repeat(" ", maxRequestBytes), 413},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, request(tc.body))
			if w.Code != tc.status {
				t.Fatalf("got %d: %s", w.Code, w.Body.String())
			}
		})
	}
	t.Run("media-type", func(t *testing.T) {
		r := request(`{"question":"q"}`)
		r.Header.Set("Content-Type", "text/plain")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 415 {
			t.Fatal(w.Code)
		}
	})
	t.Run("method", func(t *testing.T) {
		r := request(`{"question":"q"}`)
		r.Method = http.MethodGet
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 405 || w.Header().Get("Allow") != "POST" {
			t.Fatal(w.Code)
		}
	})
}

func TestQuestionAuthorizationFailures(t *testing.T) {
	f := answerFunc(func(context.Context, string, rag.Access) (string, error) {
		t.Fatal("unauthorized request reached RAG")
		return "", nil
	})
	h := testHandler(t, f)
	for _, header := range []string{"", "Bearer manager", "Basic " + testToken, "Bearer " + testToken + " extra", "Bearer " + strings.Repeat("z", 32)} {
		r := request(`{"question":"q"}`)
		r.Header.Set("Authorization", header)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	r := request(`{"question":"q"}`)
	r.Header.Add("Authorization", "Bearer "+testToken)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("duplicate header accepted")
	}
	for _, tc := range []struct {
		name   string
		access rag.Access
		err    error
		status int
	}{
		{"forbidden", rag.Access{}, ErrForbidden, 403},
		{"unknown-role", rag.Access{Role: "admin"}, nil, 403},
		{"bad-team", rag.Access{Role: "intern", Team: " "}, nil, 403},
		{"auth-service-failed", rag.Access{}, errors.New("secret diagnostic"), 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testHandler(t, f)
			h.authenticate = func(*http.Request) (rag.Access, error) { return tc.access, tc.err }
			w := httptest.NewRecorder()
			h.ServeHTTP(w, request(`{"question":"q"}`))
			if w.Code != tc.status || strings.Contains(w.Body.String(), "secret diagnostic") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
}

func TestQuestionErrorsAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"internal", errors.New("postgres secret diagnostic"), 500},
		{"deadline", fmt.Errorf("dependency: %w", context.DeadlineExceeded), 504},
		{"network-timeout", &net.DNSError{IsTimeout: true}, 504},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := testHandler(t, func(context.Context, string, rag.Access) (string, error) { return "", tc.err })
			w := httptest.NewRecorder()
			h.ServeHTTP(w, request(`{"question":"q"}`))
			if w.Code != tc.status || strings.Contains(w.Body.String(), "secret") {
				t.Fatal(w.Code, w.Body.String())
			}
		})
	}
	t.Run("own-deadline", func(t *testing.T) {
		h := testHandler(t, func(ctx context.Context, _ string, _ rag.Access) (string, error) { <-ctx.Done(); return "", ctx.Err() })
		h.timeout = 10 * time.Millisecond
		w := httptest.NewRecorder()
		h.ServeHTTP(w, request(`{"question":"q"}`))
		if w.Code != 504 {
			t.Fatal(w.Code)
		}
	})
	t.Run("client-cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		h := testHandler(t, func(ctx context.Context, _ string, _ rag.Access) (string, error) {
			cancel()
			<-ctx.Done()
			return "", ctx.Err()
		})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, request(`{"question":"q"}`).WithContext(ctx))
		if w.Body.Len() != 0 {
			t.Fatal("wrote response after client cancellation")
		}
	})
	t.Run("already-canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		h := testHandler(t, func(context.Context, string, rag.Access) (string, error) {
			t.Fatal("canceled request reached RAG")
			return "", nil
		})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, request(`{"question":"q"}`).WithContext(ctx))
		if w.Body.Len() != 0 {
			t.Fatal(w.Body.String())
		}
	})
}

type emptyEmbedder struct{}

func (emptyEmbedder) EmbedQuery(context.Context, string) ([]float32, error)         { return []float32{1}, nil }
func (emptyEmbedder) EmbedDocuments(context.Context, []string) ([][]float32, error) { return nil, nil }

type emptyStore struct{}

func (emptyStore) Search(context.Context, string, []float32, rag.Access) ([]string, error) {
	return []string{}, nil
}

func TestQuestionRealRetrieverWithoutContext(t *testing.T) {
	t.Setenv("OPENROUTER_API_KEY", "")
	retriever, err := rag.NewRetriever(emptyEmbedder{}, emptyStore{})
	if err != nil {
		t.Fatal(err)
	}
	h, err := NewQuestionHandler(retriever, testAuth(t))
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, request(`{"question":"q"}`))
	var response QuestionResponse
	_ = json.Unmarshal(w.Body.Bytes(), &response)
	if w.Code != 200 || response.Answer != ModelAPI.NoContextAnswer {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestServerConfigurationAndSlowBody(t *testing.T) {
	t.Setenv("HTTP_HOST", "::1")
	t.Setenv("HTTP_PORT", "8085")
	server, err := NewHTTPServer(answerFunc(func(context.Context, string, rag.Access) (string, error) {
		t.Error("slow or invalid request reached RAG")
		return "", nil
	}), testAuth(t))
	if err != nil {
		t.Fatal(err)
	}
	if server.Addr != "[::1]:8085" || server.ReadTimeout <= 0 || server.ReadHeaderTimeout <= 0 || server.WriteTimeout <= server.ReadTimeout+questionTimeout || server.IdleTimeout <= 0 {
		t.Fatal("unsafe server config", server)
	}
	w := httptest.NewRecorder()
	r := request(`{}`)
	r.Method = "GET"
	server.Handler.ServeHTTP(w, r)
	if w.Code != 405 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	server.Handler.ServeHTTP(w, request(`{}`))
	if w.Code != 400 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	r = request(`{}`)
	r.URL.Path = "/missing"
	server.Handler.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	// Exercise actual net/http body timeout, not just a synthetic handler error.
	testServer := httptest.NewUnstartedServer(server.Handler)
	testServer.Config.ReadHeaderTimeout = time.Second
	testServer.Config.ReadTimeout = 50 * time.Millisecond
	testServer.Config.WriteTimeout = time.Second
	testServer.Start()
	defer testServer.Close()
	conn, err := net.Dial("tcp", testServer.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, err = fmt.Fprintf(conn, "POST /question HTTP/1.1\r\nHost: localhost\r\nAuthorization: Bearer %s\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{", testToken)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 408 {
		t.Fatal(response.Status)
	}
}

func TestConstructorsAndPortValidation(t *testing.T) {
	f := answerFunc(func(context.Context, string, rag.Access) (string, error) { return "", nil })
	auth := testAuth(t)
	if _, err := NewQuestionHandler(nil, auth); err == nil {
		t.Fatal("nil answerer accepted")
	}
	var retriever *rag.Retriever
	if _, err := NewQuestionHandler(retriever, auth); err == nil {
		t.Fatal("typed nil retriever accepted")
	}
	if _, err := NewQuestionHandler(f, nil); err == nil {
		t.Fatal("nil auth accepted")
	}
	if _, err := NewHTTPServer(nil, auth); err == nil {
		t.Fatal("nil answerer accepted")
	}
	if err := StartHTTPServer(nil); err == nil {
		t.Fatal("nil retriever accepted")
	}
	t.Setenv("HTTP_DEMO_USERS", "")
	retriever, _ = rag.NewRetriever(emptyEmbedder{}, emptyStore{})
	if err := StartHTTPServer(retriever); err == nil {
		t.Fatal("server started without auth configuration")
	}
	for _, port := range []string{"bad", "-1", "0", "65536"} {
		t.Setenv("HTTP_PORT", port)
		if _, err := NewHTTPServer(f, auth); err == nil {
			t.Fatal("invalid port accepted")
		}
	}
	t.Setenv("HTTP_HOST", "")
	t.Setenv("HTTP_PORT", "")
	s, err := NewHTTPServer(f, auth)
	if err != nil || s.Addr != "localhost:8080" {
		t.Fatal(s, err)
	}
}
