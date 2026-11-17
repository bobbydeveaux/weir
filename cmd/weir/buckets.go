//go:build !nobuckets

package main

import (
	"context"

	"github.com/bobbydeveaux/weir/blob"
	"github.com/bobbydeveaux/weir/blob/bucket"
)

// The cloud storage libraries make up most of the size of this program. A
// build that will only ever keep files in Postgres can leave them out:
//
//	go build -tags nobuckets ./cmd/weir
func init() {
	openBucket = func(ctx context.Context, address string) (blob.Store, error) { return bucket.Open(ctx, address) }
}
