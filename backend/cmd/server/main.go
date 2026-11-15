package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"GO/FrontendAPI"
	"GO/rag"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	if err := godotenv.Load("../.env"); err != nil &&
		!errors.Is(err, os.ErrNotExist) {
		return err
	}

	config, err := rag.ConfigFromEnv()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := pgxpool.New(ctx, config.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return err
	}

	store, err := rag.OpenStore(ctx, pool, config)
	if err != nil {
		return err
	}

	embedder, err := rag.NewHTTPEmbedder(config)
	if err != nil {
		return err
	}

	retriever, err := rag.NewRetriever(embedder, store)
	if err != nil {
		return err
	}

	authenticate, err := FrontendAPI.DemoAuthenticatorFromEnv()
	if err != nil {
		return err
	}
	server, err := FrontendAPI.NewHTTPServer(retriever, authenticate)
	if err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close()
		}
	}()
	log.Printf("HTTP API: %s", server.Addr)
	err = server.ListenAndServe()
	stop()
	<-done
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
