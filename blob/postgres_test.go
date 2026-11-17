package blob_test

import (
	"context"
	"testing"

	"github.com/bobbydeveaux/weir/blob"
	"github.com/bobbydeveaux/weir/blob/blobtest"
	"github.com/bobbydeveaux/weir/internal/testdb"
)

func TestPostgresKeepsFiles(t *testing.T) {
	pool := testdb.New(context.Background(), t)
	small := blob.NewPostgres(pool)
	small.Max = 1 << 10
	blobtest.Run(t, blob.NewPostgres(pool), small)
	if got := blob.NewPostgres(pool).Describe(); got != "postgres" {
		t.Errorf("Describe = %q", got)
	}
}
