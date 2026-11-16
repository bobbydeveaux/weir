// Package testdb gives a test its own migrated Postgres schema, so tests
// cannot see each other's rows.
package testdb

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bobbydeveaux/weir/engine/store"
)

// New returns a pool connected to a fresh, migrated schema. The schema is
// dropped when the test ends. The test is skipped when WEIR_TEST_DATABASE_URL
// is not set.
func New(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("WEIR_TEST_DATABASE_URL")
	if base == "" {
		t.Skip("WEIR_TEST_DATABASE_URL is not set; run `make test-db` to test against Postgres")
	}
	raw := make([]byte, 6)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	schema := "weir_test_" + hex.EncodeToString(raw)

	admin, err := pgxpool.New(ctx, base)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
		admin.Close()
	})

	sep := "?"
	if strings.Contains(base, "?") {
		sep = "&"
	}
	url := base + sep + "search_path=" + schema
	if _, err := store.Migrate(ctx, url); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}
