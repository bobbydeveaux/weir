// Package store owns Weir's Postgres schema and every query against it.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate brings the database at databaseURL up to the latest schema version
// and returns the version it ended on.
func Migrate(ctx context.Context, databaseURL string) (int64, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return 0, fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	return MigrateDB(ctx, db)
}

// MigrateDB brings a database that is already open up to the latest schema
// version, and returns the version it ended on. It is for a caller that signs
// in some other way than a password in an address.
func MigrateDB(ctx context.Context, db *sql.DB) (int64, error) {
	if err := db.PingContext(ctx); err != nil {
		return 0, fmt.Errorf("connect to database: %w", err)
	}

	files, err := fs.Sub(migrationFiles, "migrations")
	if err != nil {
		return 0, fmt.Errorf("read embedded migrations: %w", err)
	}
	// A lock held for the length of the migration, so that two servers
	// starting together take turns instead of both making the same table.
	// It asks for the lock once a second, for up to five minutes.
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 300))
	if err != nil {
		return 0, fmt.Errorf("prepare migrations: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, files, goose.WithSessionLocker(locker))
	if err != nil {
		return 0, fmt.Errorf("prepare migrations: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return 0, fmt.Errorf("apply migrations: %w", err)
	}
	version, err := provider.GetDBVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	return version, nil
}
