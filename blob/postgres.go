package blob

import (
	"bytes"
	"context"
	"errors"
	"io"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres keeps files in the database Weir already uses. It suits a small
// install and modest files; each file is held in memory while it is written
// or read. For more, configure a bucket.
type Postgres struct {
	pool *pgxpool.Pool
	// Max is the largest file accepted. Zero means DefaultMax.
	Max int64
}

// NewPostgres returns a store that keeps files in the blobs table.
func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func (p *Postgres) max() int64 {
	if p.Max > 0 {
		return p.Max
	}
	return DefaultMax
}

// Put implements Store.
func (p *Postgres) Put(ctx context.Context, key string, body io.Reader, contentType string) (Stored, error) {
	if err := CheckKey(key); err != nil {
		return Stored{}, err
	}
	m := Measure(body, p.max())
	data, err := io.ReadAll(m)
	if err != nil {
		return Stored{}, err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	got := m.Result()
	_, err = p.pool.Exec(ctx, `
		INSERT INTO blobs (key, content_type, size, sha256, data) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (key) DO UPDATE SET content_type = EXCLUDED.content_type, size = EXCLUDED.size,
			sha256 = EXCLUDED.sha256, data = EXCLUDED.data, created_at = now()`,
		key, contentType, got.Size, got.SHA256, data)
	return got, err
}

// Get implements Store.
func (p *Postgres) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := CheckKey(key); err != nil {
		return nil, err
	}
	var data []byte
	err := p.pool.QueryRow(ctx, `SELECT data FROM blobs WHERE key = $1`, key).Scan(&data)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// Delete implements Store.
func (p *Postgres) Delete(ctx context.Context, key string) error {
	if err := CheckKey(key); err != nil {
		return err
	}
	_, err := p.pool.Exec(ctx, `DELETE FROM blobs WHERE key = $1`, key)
	return err
}

// Describe implements Store.
func (p *Postgres) Describe() string { return "postgres" }

// Close implements Store. The database belongs to the caller.
func (p *Postgres) Close() error { return nil }
