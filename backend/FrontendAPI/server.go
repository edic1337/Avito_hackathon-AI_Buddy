package FrontendAPI

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"GO/rag"
)

// NewHTTPServer lets the application's entry point own ListenAndServe/Shutdown
// and inject its existing authentication. No database pools are created per request.
func NewHTTPServer(answerer Answerer, authenticate Authenticator) (*http.Server, error) {
	handler, err := NewQuestionHandler(answerer, authenticate)
	if err != nil {
		return nil, err
	}
	host := os.Getenv("HTTP_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("HTTP_PORT")
	if port == "" {
		port = "8080"
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return nil, fmt.Errorf("HTTP_PORT must be between 1 and 65535")
	}
	mux := http.NewServeMux()
	mux.Handle("POST /question", handler)
	return &http.Server{
		Addr: net.JoinHostPort(host, port), Handler: mux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// Includes reading the body, the entire RAG/LLM deadline and response writes.
		WriteTimeout: questionTimeout + 15*time.Second,
		IdleTimeout:  60 * time.Second,
	}, nil
}

// StartHTTPServer retains the colleague's entry point and uses configured demo
// authentication. Load .env in main before calling; never use production tokens
// as a frontend role-selection menu.
func StartHTTPServer(retriever *rag.Retriever) error {
	if retriever == nil {
		return errors.New("retriever is required")
	}
	authenticate, err := DemoAuthenticatorFromEnv()
	if err != nil {
		return err
	}
	server, err := NewHTTPServer(retriever, authenticate)
	if err != nil {
		return err
	}
	if err = server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
