// Package blob is where Weir keeps files: anything too large or too binary to
// sit in a step's output. The storage behind it is a setting. Postgres is the
// default, so that a fresh install needs nothing else; a bucket (S3, Google
// Cloud Storage, Azure Blob Storage, or a directory) takes over when one is
// configured. Callers use Store and do not know which it is.
//
// A store holds bytes under a key and nothing else. What a file is, who owns
// it and whether anyone may read it are recorded by whatever uses the store.
package blob

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"regexp"
	"strings"
)

// DefaultMax is the largest file a store accepts unless told otherwise.
const DefaultMax = 32 << 20

var (
	// ErrNotFound means there is no file under that key.
	ErrNotFound = errors.New("no such file")
	// ErrTooLarge means a file is larger than the store accepts. Nothing of
	// it is kept.
	ErrTooLarge = errors.New("the file is too large")
)

// Stored describes a file that has been kept.
type Stored struct {
	Size int64
	// SHA256 is the hex digest of the file's bytes.
	SHA256 string
}

// Store keeps files under keys.
type Store interface {
	// Put keeps the bytes read from body under key, replacing anything
	// already there. If body is larger than the store accepts it returns
	// ErrTooLarge and keeps nothing.
	Put(ctx context.Context, key string, body io.Reader, contentType string) (Stored, error)
	// Get returns the file under key. The caller must close it. It returns
	// ErrNotFound if there is none.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete removes the file under key. Removing one that is not there is
	// not an error.
	Delete(ctx context.Context, key string) error
	// Describe says where files are kept, for a person: "postgres", or the
	// bucket's address with nothing secret in it.
	Describe() string
	// Close releases anything the store holds open.
	Close() error
}

var segment = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)

// CheckKey reports whether key is one a store accepts: plain segments joined
// by slashes, such as "acme/runs/41b5/report.pdf". A key cannot begin with a
// slash, be empty, or contain "." or ".." segments, so it cannot name
// anything outside the store.
func CheckKey(key string) error {
	if key == "" || len(key) > 512 {
		return errors.New("a file's key must be between 1 and 512 characters")
	}
	for _, part := range strings.Split(key, "/") {
		if !segment.MatchString(part) || strings.Contains(part, "..") {
			return fmt.Errorf("the key %q is not made of plain names joined by slashes", key)
		}
	}
	return nil
}

// Measure wraps body so that reading it counts its bytes and hashes them, and
// fails with ErrTooLarge once more than max have been read. Call Result when
// the body has been read to the end.
func Measure(body io.Reader, max int64) *Measured {
	return &Measured{body: body, max: max, hash: sha256.New()}
}

// Measured is a body being counted and hashed as it is read.
type Measured struct {
	body io.Reader
	max  int64
	size int64
	hash hash.Hash
}

func (m *Measured) Read(p []byte) (int, error) {
	n, err := m.body.Read(p)
	m.size += int64(n)
	if m.size > m.max {
		return n, fmt.Errorf("%w: more than %d MB", ErrTooLarge, m.max>>20)
	}
	m.hash.Write(p[:n])
	return n, err
}

// Result returns the size and digest of what was read.
func (m *Measured) Result() Stored {
	return Stored{Size: m.size, SHA256: hex.EncodeToString(m.hash.Sum(nil))}
}
