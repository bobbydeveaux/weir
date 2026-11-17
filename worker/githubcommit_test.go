package worker

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bobbydeveaux/weir/engine"
)

func TestACommitCannotBeUsedToWriteOutsideTheRepository(t *testing.T) {
	remote := origin(t)
	ctx := context.Background()
	outside := t.TempDir()
	// The repository itself holds a link that points outside it.
	link := exec.Command("git", "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "--quiet", "-m", "a link")
	if err := os.Symlink(outside, filepath.Join(remote, "elsewhere")); err != nil {
		t.Fatal(err)
	}
	add := exec.Command("git", "add", "-A")
	add.Dir, link.Dir = remote, remote
	link.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_CONFIG_NOSYSTEM=1")
	if out, err := add.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if out, err := link.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	_ = os.WriteFile(filepath.Join(outside, "precious.txt"), []byte("keep"), 0o644)

	for name, files := range map[string][]engine.NewFile{
		"goes through a link":        {{Path: "elsewhere/planted.txt", Data: []byte("x")}},
		"goes through a link ":       {{Path: "elsewhere/precious.txt", Deleted: true}},
		"repository's own records":   {{Path: ".git/hooks/pre-commit", Data: []byte("#!/bin/sh\n")}},
		"repository's own records ":  {{Path: "vendor/x/.git/config", Data: []byte("x")}},
		"stays inside its directory": {{Path: "../escape.txt", Data: []byte("x")}},
	} {
		_, _, err := commit(ctx, "", remote, "main", "weir/bad", "m", "", "", files)
		if err == nil || !strings.Contains(err.Error(), strings.TrimSpace(name)) {
			t.Errorf("committing %s = %v, want it refused as %q", files[0].Path, err, strings.TrimSpace(name))
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "planted.txt")); err == nil {
		t.Error("a file was written outside the repository")
	}
	if _, err := os.Stat(filepath.Join(outside, "precious.txt")); err != nil {
		t.Error("a file outside the repository was deleted")
	}
	// Files that make no difference are not a commit.
	if _, changed, err := commit(ctx, "", remote, "main", "weir/same", "m", "", "", []engine.NewFile{{Path: "main.go", Data: []byte("package main\n")}}); err != nil || changed {
		t.Errorf("unchanged files: changed %v, %v", changed, err)
	}
}
