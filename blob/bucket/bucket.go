// Package bucket keeps Weir's files in a bucket: Amazon S3, Google Cloud
// Storage, Azure Blob Storage, or a directory on disk.
//
// The bucket is named by an address:
//
//	s3://my-bucket?region=eu-west-1
//	gs://my-bucket
//	azblob://my-container
//	file:///var/lib/weir/files
//
// Weir holds no key for the bucket. Each cloud's own library finds the
// credentials the platform provides: an IAM role on AWS, Workload Identity on
// Google Cloud, a managed identity on Azure, or whatever its usual
// environment variables say.
//
// It is a separate package so that only a program that wants buckets pays for
// the cloud libraries.
package bucket

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"

	gcblob "gocloud.dev/blob"
	_ "gocloud.dev/blob/azureblob" // azblob://
	_ "gocloud.dev/blob/fileblob"  // file://
	_ "gocloud.dev/blob/gcsblob"   // gs://
	_ "gocloud.dev/blob/memblob"   // mem://, for tests
	_ "gocloud.dev/blob/s3blob"    // s3://
	"gocloud.dev/gcerrors"

	"github.com/bobbydeveaux/weir/blob"
)

// Store keeps files in a bucket.
type Store struct {
	bucket *gcblob.Bucket
	where  string
	// Max is the largest file accepted. Zero means blob.DefaultMax.
	Max int64
}

// Open opens the bucket at address.
func Open(ctx context.Context, address string) (*Store, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, fmt.Errorf("the bucket address is not understood: %w", err)
	}
	switch u.Scheme {
	case "s3", "gs", "azblob", "file", "mem":
	default:
		return nil, fmt.Errorf("the bucket address must begin s3://, gs://, azblob:// or file://, not %q", u.Scheme+"://")
	}
	b, err := gcblob.OpenBucket(ctx, address)
	if err != nil {
		return nil, fmt.Errorf("open the bucket: %w", err)
	}
	// Settings after the ? can carry things not to show, so they are left out.
	where := u.Scheme + "://" + u.Host + u.Path
	return &Store{bucket: b, where: where}, nil
}

func (s *Store) max() int64 {
	if s.Max > 0 {
		return s.Max
	}
	return blob.DefaultMax
}

// Put implements blob.Store.
func (s *Store) Put(ctx context.Context, key string, body io.Reader, contentType string) (blob.Stored, error) {
	if err := blob.CheckKey(key); err != nil {
		return blob.Stored{}, err
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// Cancelling the context before the writer is closed abandons the upload,
	// which is how a file that turns out to be too large leaves nothing behind.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	w, err := s.bucket.NewWriter(ctx, key, &gcblob.WriterOptions{ContentType: contentType})
	if err != nil {
		return blob.Stored{}, err
	}
	m := blob.Measure(body, s.max())
	if _, err := io.Copy(w, m); err != nil {
		cancel()
		_ = w.Close()
		return blob.Stored{}, err
	}
	if err := w.Close(); err != nil {
		return blob.Stored{}, err
	}
	return m.Result(), nil
}

// Get implements blob.Store.
func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := blob.CheckKey(key); err != nil {
		return nil, err
	}
	r, err := s.bucket.NewReader(ctx, key, nil)
	if gcerrors.Code(err) == gcerrors.NotFound {
		return nil, blob.ErrNotFound
	}
	return r, err
}

// Delete implements blob.Store.
func (s *Store) Delete(ctx context.Context, key string) error {
	if err := blob.CheckKey(key); err != nil {
		return err
	}
	err := s.bucket.Delete(ctx, key)
	if gcerrors.Code(err) == gcerrors.NotFound {
		return nil
	}
	return err
}

// Describe implements blob.Store.
func (s *Store) Describe() string { return s.where }

// Close implements blob.Store.
func (s *Store) Close() error {
	if s.bucket == nil {
		return errors.New("the bucket is not open")
	}
	return s.bucket.Close()
}
