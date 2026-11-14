// Command rag is a local/admin tool, not a public API. CLI role/team are trusted
// demo inputs; production handlers must obtain them from authentication.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"GO/rag"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: rag <migrate|ingest|import|search|ask> [flags]")
	}
	flags := flag.NewFlagSet(args[0], flag.ContinueOnError)
	envFile := flags.String("env", "../.env", "optional .env path")
	file := flags.String("file", "", "input JSON")
	key := flags.String("source-key", "", "logical key for import")
	question := flags.String("question", "", "question")
	role := flags.String("role", "intern", "demo role: intern/buddy/manager")
	team := flags.String("team", "", "demo team")
	timeout := flags.Duration("timeout", 2*time.Minute, "total operation timeout")
	if err := flags.Parse(args[1:]); err != nil {
		return err
	}
	switch args[0] {
	case "migrate", "ingest", "import", "search", "ask":
	default:
		return fmt.Errorf("unknown command %q", args[0])
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if *timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	if err := godotenv.Load(*envFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load env: %w", err)
	}
	config, err := rag.ConfigFromEnv()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	pool, err := pgxpool.New(ctx, config.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if args[0] == "migrate" {
		return rag.Migrate(ctx, pool)
	}
	store, err := rag.OpenStore(ctx, pool, config)
	if err != nil {
		return err
	}
	if args[0] == "import" {
		f, err := os.Open(*file)
		if err != nil {
			return err
		}
		defer f.Close()
		id, err := store.ImportJSON(ctx, f, *key)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, id)
		return err
	}
	embedder, err := rag.NewHTTPEmbedder(config)
	if err != nil {
		return err
	}
	if args[0] == "ingest" {
		f, err := os.Open(*file)
		if err != nil {
			return err
		}
		defer f.Close()
		var input struct {
			Document rag.Document `json:"document"`
			Texts    []string     `json:"texts"`
		}
		const maxInput = 64 << 20
		raw, err := io.ReadAll(io.LimitReader(f, maxInput+1))
		if err != nil {
			return err
		}
		if len(raw) > maxInput {
			return fmt.Errorf("input exceeds 64 MiB")
		}
		if err = json.Unmarshal(raw, &input); err != nil {
			return err
		}
		id, err := store.SaveDocument(ctx, input.Document, input.Texts, embedder)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, id)
		return err
	}
	retriever, err := rag.NewRetriever(embedder, store)
	if err != nil {
		return err
	}
	access := rag.Access{Role: *role, Team: *team}
	if args[0] == "ask" {
		answer, err := retriever.Answer(ctx, *question, access)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(out, answer)
		return err
	}
	texts, err := retriever.Retrieve(ctx, *question, access)
	if err != nil {
		return err
	}
	return json.NewEncoder(out).Encode(texts)
}
