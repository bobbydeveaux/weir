package store

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

// testDatabaseURL returns the database to test against, or skips the test.
// Store tests run against a real Postgres, never a fake.
func testDatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv("WEIR_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("WEIR_TEST_DATABASE_URL is not set; run `make test-db` to test against Postgres")
	}
	return url
}

func TestMigrateCreatesTheDefaultWorkspace(t *testing.T) {
	url := testDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	version, err := Migrate(ctx, url)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if version < 1 {
		t.Fatalf("schema version = %d, want at least 1", version)
	}

	// Running it again must be a no-op.
	again, err := Migrate(ctx, url)
	if err != nil {
		t.Fatalf("Migrate a second time: %v", err)
	}
	if again != version {
		t.Fatalf("second Migrate ended on version %d, want %d", again, version)
	}

	db, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var slug string
	if err := db.QueryRowContext(ctx, `SELECT slug FROM workspaces`).Scan(&slug); err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	if slug != "default" {
		t.Fatalf("workspace slug = %q, want default", slug)
	}
}
