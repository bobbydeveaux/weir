package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync"
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

// Several servers starting at once all migrate. They must take turns. This
// passes without the lock too, on a quiet machine: the lock is what makes it
// a guarantee, and this shows that taking it does not get in the way.
func TestMigrationsStartedTogetherTakeTurns(t *testing.T) {
	base := testDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	admin, err := sql.Open("pgx", base)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	// A database of its own, with nothing in it.
	name := fmt.Sprintf("weir_migrate_%d", time.Now().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)") })
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name

	var wg sync.WaitGroup
	errs := make([]error, 5)
	versions := make([]int64, 5)
	for i := range errs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			versions[i], errs[i] = Migrate(ctx, parsed.String())
		}()
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Errorf("migration %d: %v", i, err)
		}
		if versions[i] != versions[0] {
			t.Errorf("migration %d ended on version %d, and the first on %d", i, versions[i], versions[0])
		}
	}
}
