package archive

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestADirectoryComesBackAsItWas(t *testing.T) {
	src := t.TempDir()
	_ = os.MkdirAll(filepath.Join(src, "a", "b"), 0o755)
	_ = os.MkdirAll(filepath.Join(src, "empty"), 0o755)
	_ = os.WriteFile(filepath.Join(src, "a", "b", "file.txt"), []byte("hello"), 0o644)
	_ = os.WriteFile(filepath.Join(src, "run.sh"), []byte("#!/bin/sh\n"), 0o755)
	_ = os.Symlink("a/b/file.txt", filepath.Join(src, "link"))

	var buf bytes.Buffer
	if err := Pack(src, &buf, 1<<20); err != nil {
		t.Fatalf("Pack: %v", err)
	}
	dst := t.TempDir()
	if err := Unpack(&buf, dst); err != nil {
		t.Fatalf("Unpack: %v", err)
	}
	if data, _ := os.ReadFile(filepath.Join(dst, "a", "b", "file.txt")); string(data) != "hello" {
		t.Errorf("file = %q", data)
	}
	if info, err := os.Stat(filepath.Join(dst, "run.sh")); err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Errorf("run.sh is no longer executable: %v, %v", info, err)
	}
	if target, _ := os.Readlink(filepath.Join(dst, "link")); target != "a/b/file.txt" {
		t.Errorf("link = %q", target)
	}
	if info, err := os.Stat(filepath.Join(dst, "empty")); err != nil || !info.IsDir() {
		t.Errorf("the empty directory is missing: %v", err)
	}

	if err := Pack(src, &bytes.Buffer{}, 10); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a directory over the limit = %v", err)
	}
}

func TestAnArchiveCannotWriteOutsideItsDirectory(t *testing.T) {
	var buf bytes.Buffer
	zip := gzip.NewWriter(&buf)
	out := tar.NewWriter(zip)
	_ = out.WriteHeader(&tar.Header{Name: "../escape.txt", Typeflag: tar.TypeReg, Mode: 0o644, Size: 1})
	_, _ = out.Write([]byte("x"))
	_ = out.Close()
	_ = zip.Close()
	dir := t.TempDir()
	if err := Unpack(&buf, dir); err == nil {
		t.Error("an archive that names a path outside the directory was unpacked")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "escape.txt")); err == nil {
		t.Error("a file was written outside the directory")
	}
}
