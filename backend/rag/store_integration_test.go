package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// These tests only use TEST_DATABASE_URL, never DATABASE_URL. Each test owns an
// isolated collection and removes only its rows. Migrations require DDL rights.
func integrationStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set TEST_DATABASE_URL to a disposable PostgreSQL + pgvector database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err = Migrate(ctx, pool); err != nil {
		t.Fatal("repeat migration:", err)
	}
	c := DefaultConfig()
	c.Collection = fmt.Sprintf("test_%d", time.Now().UnixNano())
	s, err := OpenStore(ctx, pool, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = pool.Exec(ctx, `DELETE FROM rag.documents WHERE collection_id=$1`, s.collectionID)
		_, _ = pool.Exec(ctx, `DELETE FROM rag.collections WHERE id=$1`, s.collectionID)
	})
	return s, ctx
}

func testDocument(key, role, team string) Document {
	return Document{SourceKey: key, Title: key, Text: "source text", SourceURL: "demo://" + key, Origin: "synthetic", MinRole: role, Team: team}
}

func saveVector(t *testing.T, s *Store, ctx context.Context, d Document, body string, v []float32) int64 {
	t.Helper()
	id, err := s.save(ctx, d, []string{body}, []string{""}, []*int{nil}, [][]float32{v})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestIntegrationHybridACL(t *testing.T) {
	s, ctx := integrationStore(t)
	saveVector(t, s, ctx, testDocument("general", "intern", ""), "Общие сведения", unitVector(0))
	v := unitVector(0)
	v[0] = 0.8
	v[1] = 0.6
	saveVector(t, s, ctx, testDocument("tool", "intern", ""), "Доступ к avito-cli", v)
	saveVector(t, s, ctx, testDocument("secret", "buddy", ""), "avito-cli для наставника", unitVector(0))
	saveVector(t, s, ctx, testDocument("other-team", "intern", "other"), "avito-cli другой команды", unitVector(0))
	saveVector(t, s, ctx, testDocument("own-team", "intern", "mine"), "avito-cli своей команды", unitVector(0))
	saveVector(t, s, ctx, testDocument("weak", "intern", ""), "avito-cli avito-cli", unitVector(1))
	for _, tc := range []struct {
		access    Access
		count     int
		forbidden []string
	}{
		{Access{"intern", ""}, 2, []string{"secret", "other-team", "own-team", "weak"}},
		{Access{"intern", "mine"}, 3, []string{"secret", "other-team", "weak"}},
		{Access{"buddy", ""}, 3, []string{"other-team", "own-team", "weak"}},
		{Access{"manager", "mine"}, 4, []string{"other-team", "weak"}},
	} {
		t.Run(tc.access.Role+tc.access.Team, func(t *testing.T) {
			texts, err := s.Search(ctx, "avito-cli", unitVector(0), tc.access)
			if err != nil {
				t.Fatal(err)
			}
			if len(texts) != tc.count {
				t.Fatalf("got %d texts, want %d: %v", len(texts), tc.count, texts)
			}
			for _, text := range texts {
				for _, forbidden := range tc.forbidden {
					if strings.Contains(text, forbidden) {
						t.Fatalf("unauthorized or irrelevant context: %s", text)
					}
				}
				if !strings.Contains(text, "не регламент Авито") {
					t.Fatal("lost origin")
				}
			}
			if tc.access.Role == "intern" && tc.access.Team == "" && !strings.HasPrefix(texts[0], "tool\n") {
				t.Fatal("FTS did not boost keyword match")
			}
		})
	}
	// Closed high-ranking rows must not consume candidate slots.
	small := *s
	small.config.TopK = 1
	small.config.CandidateLimit = 1
	texts, err := small.Search(ctx, "avito-cli", unitVector(0), Access{"intern", ""})
	if err != nil || len(texts) != 1 {
		t.Fatal(texts, err)
	}
	if err = s.DeactivateDocument(ctx, "tool"); err != nil {
		t.Fatal(err)
	}
	texts, err = s.Search(ctx, "avito-cli", unitVector(0), Access{"intern", ""})
	if err != nil || len(texts) != 1 {
		t.Fatal(texts, err)
	}
	small.config.MaxContextRunes = 1
	texts, err = small.Search(ctx, "avito-cli", unitVector(0), Access{"intern", ""})
	if err != nil || len(texts) != 0 {
		t.Fatal("context budget", texts, err)
	}
	texts, err = s.Search(ctx, "unrelated", unitVector(2), Access{"intern", ""})
	if err != nil || len(texts) != 0 {
		t.Fatal("cosine threshold", texts, err)
	}
}

func TestIntegrationSaveImportRollback(t *testing.T) {
	s, ctx := integrationStore(t)
	d := testDocument("vpn", "intern", "")
	e := &fakeEmbedder{}
	id, err := s.SaveDocument(ctx, d, []string{"первая", "вторая"}, e)
	if err != nil {
		t.Fatal(err)
	}
	if e.texts[0] != "vpn\nпервая" {
		t.Fatal("title not embedded")
	}
	id2, err := s.SaveDocument(ctx, d, []string{"первая", "вторая"}, e)
	if err != nil || id2 != id {
		t.Fatal(id, id2, err)
	}
	var count int
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM rag.chunks WHERE doc_id=$1`, id).Scan(&count); err != nil || count != 2 {
		t.Fatal("duplicate texts", count, err)
	}
	// SQL rejects NUL in the second insertion, after UPSERT/DELETE/first insert.
	bad := d
	bad.MinRole = "manager"
	if _, err = s.SaveDocument(ctx, bad, []string{"new", "bad\x00text"}, e); err == nil {
		t.Fatal("expected SQL failure")
	}
	texts, err := s.Search(ctx, "первая", unitVector(0), Access{"intern", ""})
	if err != nil || len(texts) != 2 || strings.Contains(strings.Join(texts, ""), "new") {
		t.Fatal("rollback lost old data/ACL", texts, err)
	}
	d.MinRole = "buddy"
	if _, err = s.SaveDocument(ctx, d, []string{"updated"}, e); err != nil {
		t.Fatal(err)
	}
	texts, err = s.Search(ctx, "updated", unitVector(0), Access{"intern", ""})
	if err != nil || len(texts) != 0 {
		t.Fatal("metadata ACL not updated", texts, err)
	}
	if err = s.pool.QueryRow(ctx, `SELECT count(*) FROM rag.chunks WHERE doc_id=$1`, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("stale passages", err)
	}
	p := sampleExport(t)
	raw, _ := json.Marshal(p)
	importID, err := s.ImportJSON(ctx, strings.NewReader(string(raw)), "imported")
	if err != nil {
		t.Fatal(err)
	}
	var section string
	var tokenCount int
	if err = s.pool.QueryRow(ctx, `SELECT section,token_count FROM rag.chunks WHERE doc_id=$1`, importID).Scan(&section, &tokenCount); err != nil || section != "VPN" || tokenCount != 12 {
		t.Fatal(section, tokenCount, err)
	}
	if _, err = s.ImportJSON(ctx, strings.NewReader("{}"), "imported"); err == nil {
		t.Fatal("bad import accepted")
	}
	// Same collection name with a different indexing profile must fail.
	c := s.config
	c.EmbeddingRevision = "other"
	if _, err = OpenStore(ctx, s.pool, c); err == nil {
		t.Fatal("profile mismatch accepted")
	}
	if _, err = OpenStore(ctx, s.pool, s.config); err != nil {
		t.Fatal("same profile rejected", err)
	}
	other, otherCtx := integrationStore(t)
	texts, err = other.Search(otherCtx, "VPN", unitVector(0), Access{"manager", ""})
	if err != nil || len(texts) != 0 {
		t.Fatal("collection leak", texts, err)
	}
}

func TestIntegrationConcurrentReplace(t *testing.T) {
	s, ctx := integrationStore(t)
	d := testDocument("concurrent", "intern", "")
	errors := make(chan error, 4)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := s.SaveDocument(ctx, d, []string{fmt.Sprintf("version%d/a", i), fmt.Sprintf("version%d/b", i)}, &fakeEmbedder{})
			errors <- err
		}(i)
	}
	wg.Wait()
	close(errors)
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.pool.Query(ctx, `SELECT c.text FROM rag.chunks c JOIN rag.documents d ON d.id=c.doc_id WHERE d.collection_id=$1 ORDER BY position`, s.collectionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var texts []string
	for rows.Next() {
		var text string
		if err = rows.Scan(&text); err != nil {
			t.Fatal(err)
		}
		texts = append(texts, text)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(texts) != 2 || strings.Split(texts[0], "/")[0] != strings.Split(texts[1], "/")[0] {
		t.Fatal("mixed versions", texts)
	}
}
