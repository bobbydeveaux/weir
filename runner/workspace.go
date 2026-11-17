package runner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	ignore "github.com/sabhiram/go-gitignore"
)

// Workspace asks for an agent's working directory to be prepared before it
// runs. The agent works in that directory, and what it changed is returned
// as the result's files.
type Workspace struct {
	// Prepare fills dir, which is new and empty.
	Prepare func(ctx context.Context, dir string) (Prepared, error)
}

// Prepared says what a prepared directory holds.
type Prepared struct {
	// Ignore lists paths that are never kept, in the form a .gitignore uses:
	// build output, installed dependencies and the like. It is read before
	// the agent runs, so nothing the agent does can change it.
	Ignore []string
}

// tree is what a directory held: for each path, the digest of an ordinary
// file, or "link:" and the target of a link.
type tree struct {
	entries map[string]string
	skip    *ignore.GitIgnore
}

// alwaysSkipped are directories whose contents are never part of what an
// agent changed. A repository's own records are the main one: what is in
// them after an agent has run is not to be trusted or used.
var alwaysSkipped = map[string]bool{".git": true}

// snapshot records what dir holds, leaving out what is ignored.
func snapshot(dir string, patterns []string) (*tree, error) {
	t := &tree{entries: map[string]string{}, skip: ignore.CompileIgnoreLines(patterns...)}
	err := t.walk(dir, func(name, p string, d fs.DirEntry) error {
		sum, err := describe(p, d)
		if err != nil {
			return err
		}
		t.entries[name] = sum
		return nil
	})
	return t, err
}

// walk visits every path in dir that is not ignored.
func (t *tree) walk(dir string, visit func(name, p string, d fs.DirEntry) error) error {
	return filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "." {
			return err
		}
		name := filepath.ToSlash(rel)
		if d.IsDir() {
			if alwaysSkipped[d.Name()] || t.skip.MatchesPath(name+"/") {
				return filepath.SkipDir
			}
			return nil
		}
		if t.skip.MatchesPath(name) {
			return nil
		}
		return visit(name, p, d)
	})
}

// describe returns the digest of an ordinary file, or the target of a link.
func describe(p string, d fs.DirEntry) (string, error) {
	if d.Type()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(p)
		return "link:" + target, err
	}
	if !d.Type().IsRegular() {
		return "other", nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// changes returns the files in dir that were added, changed or deleted since
// the snapshot was taken. Only ordinary files can be kept: a new or changed
// link, or anything else that is not a file, is refused.
func (t *tree) changes(dir string, limit WriteLimit) ([]File, error) {
	var files []File
	var total int64
	seen := map[string]bool{}
	add := func(f File) error {
		if limit.MaxFiles > 0 && len(files) >= limit.MaxFiles {
			return fmt.Errorf("the agent changed more than %d files", limit.MaxFiles)
		}
		if total += int64(len(f.Data)); limit.MaxBytes > 0 && total > limit.MaxBytes {
			return fmt.Errorf("the agent changed more than %d MB of files", limit.MaxBytes>>20)
		}
		files = append(files, f)
		return nil
	}
	err := t.walk(dir, func(name, p string, d fs.DirEntry) error {
		seen[name] = true
		clean, err := CleanPath(name)
		if err != nil {
			return err
		}
		now, err := describe(p, d)
		if err != nil {
			return err
		}
		if now == t.entries[name] {
			return nil
		}
		if !d.Type().IsRegular() {
			return fmt.Errorf("the agent made %s something other than an ordinary file, which cannot be kept", clean)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return add(File{Name: clean, Data: data})
	})
	if err != nil {
		return nil, err
	}
	var gone []string
	for name := range t.entries {
		if !seen[name] {
			gone = append(gone, name)
		}
	}
	sort.Strings(gone)
	for _, name := range gone {
		if err := add(File{Name: name, Deleted: true}); err != nil {
			return nil, err
		}
	}
	return files, nil
}
