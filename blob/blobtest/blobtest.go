// Package blobtest holds the tests every kind of storage must pass, so that
// Postgres and each bucket behave the same.
package blobtest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/bobbydeveaux/weir/blob"
)

// Run puts a store through its paces. small is a store that accepts files of
// at most 1 KB, for checking that a file which is too large leaves nothing.
func Run(t *testing.T, store, small blob.Store) {
	t.Helper()
	ctx := context.Background()
	read := func(key string) (string, error) {
		r, err := store.Get(ctx, key)
		if err != nil {
			return "", err
		}
		defer r.Close()
		raw, err := io.ReadAll(r)
		return string(raw), err
	}

	// What goes in comes out, with its size and digest reported.
	body := "a treaty, in full\x00with a byte that is not text"
	sum := sha256.Sum256([]byte(body))
	got, err := store.Put(ctx, "acme/runs/41b5/treaty.pdf", strings.NewReader(body), "application/pdf")
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got.Size != int64(len(body)) || got.SHA256 != hex.EncodeToString(sum[:]) {
		t.Errorf("Put reported %+v, want %d bytes and the digest of the body", got, len(body))
	}
	if back, err := read("acme/runs/41b5/treaty.pdf"); err != nil || back != body {
		t.Errorf("Get = %q, %v", back, err)
	}

	// Putting again replaces; other keys are untouched.
	if _, err := store.Put(ctx, "acme/runs/41b5/treaty.pdf", strings.NewReader("second"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Put(ctx, "rival/runs/41b5/treaty.pdf", strings.NewReader("theirs"), ""); err != nil {
		t.Fatal(err)
	}
	if back, _ := read("acme/runs/41b5/treaty.pdf"); back != "second" {
		t.Errorf("after a second Put the file is %q", back)
	}
	if back, _ := read("rival/runs/41b5/treaty.pdf"); back != "theirs" {
		t.Errorf("another key was changed: %q", back)
	}

	// Missing files, and deleting.
	if _, err := read("acme/nothing-here"); !errors.Is(err, blob.ErrNotFound) {
		t.Errorf("Get of a missing file: %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, "acme/runs/41b5/treaty.pdf"); err != nil {
		t.Errorf("Delete: %v", err)
	}
	if _, err := read("acme/runs/41b5/treaty.pdf"); !errors.Is(err, blob.ErrNotFound) {
		t.Errorf("Get after Delete: %v, want ErrNotFound", err)
	}
	if err := store.Delete(ctx, "acme/runs/41b5/treaty.pdf"); err != nil {
		t.Errorf("deleting a file that is not there: %v, want no error", err)
	}
	if back, _ := read("rival/runs/41b5/treaty.pdf"); back != "theirs" {
		t.Errorf("deleting one key removed another: %q", back)
	}

	// A key cannot name anything outside the store.
	for _, key := range []string{"", "/etc/passwd", "../outside", "acme/../rival/x", "acme//x", "acme/./x", "acme/x/", ".hidden", "a b",
		"acme/..x", strings.Repeat("k", 600)} {
		if _, err := store.Put(ctx, key, strings.NewReader("x"), ""); err == nil {
			t.Errorf("Put accepted the key %q", key)
		}
		if _, err := store.Get(ctx, key); err == nil || errors.Is(err, blob.ErrNotFound) {
			t.Errorf("Get of the key %q: %v, want the key refused", key, err)
		}
	}

	// A file that is too large is refused, and nothing of it is kept.
	big := bytes.Repeat([]byte("x"), 4<<10)
	if _, err := small.Put(ctx, "acme/too-big", bytes.NewReader(big), ""); !errors.Is(err, blob.ErrTooLarge) {
		t.Errorf("Put of a file over the limit: %v, want ErrTooLarge", err)
	}
	if _, err := small.Get(ctx, "acme/too-big"); !errors.Is(err, blob.ErrNotFound) {
		t.Errorf("part of a file that was too large was kept: %v", err)
	}
	if _, err := small.Put(ctx, "acme/fits", bytes.NewReader(big[:512]), ""); err != nil {
		t.Errorf("a file under the limit was refused: %v", err)
	}
}
