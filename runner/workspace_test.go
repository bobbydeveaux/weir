package runner

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// repo fills a directory the way a checkout would: source, a record
// directory that must never be read back, and an ignore list.
func repo(_ context.Context, dir string) (Prepared, error) {
	for name, body := range map[string]string{
		"main.go":         "package main\n",
		"old.go":          "package main // going\n",
		"docs/readme.md":  "# Readme\n",
		".git/config":     "[core]\n",
		"untouched.txt":   "same\n",
		"build/cache.bin": "stale\n",
	} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			return Prepared{}, err
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			return Prepared{}, err
		}
	}
	return Prepared{Ignore: []string{"build/", "node_modules/", "*.log"}}, nil
}

func TestAnAgentInAPreparedDirectoryReturnsOnlyWhatItChanged(t *testing.T) {
	var args []string
	var dir string
	work := func(d string) {
		_ = os.WriteFile(filepath.Join(d, "main.go"), []byte("package main // fixed\n"), 0o600)
		_ = os.WriteFile(filepath.Join(d, "docs", "new.md"), []byte("# New\n"), 0o600)
		_ = os.Remove(filepath.Join(d, "old.go"))
		// None of these are the agent's work to keep.
		_ = os.WriteFile(filepath.Join(d, ".git", "config"), []byte("[core]\n\tfsmonitor = ./evil\n"), 0o600)
		_ = os.MkdirAll(filepath.Join(d, "node_modules", "left-pad"), 0o700)
		_ = os.WriteFile(filepath.Join(d, "node_modules", "left-pad", "index.js"), []byte("x"), 0o600)
		_ = os.WriteFile(filepath.Join(d, "build", "cache.bin"), []byte("fresh"), 0o600)
		_ = os.WriteFile(filepath.Join(d, "test.log"), []byte("ok"), 0o600)
		// An agent that rewrites the ignore list hides nothing by it.
		_ = os.WriteFile(filepath.Join(d, ".gitignore"), []byte("docs/\n"), 0o600)
	}
	c := &Claude{run: func(_ context.Context, _ string, a, _ []string, _, d string) ([]byte, error) {
		args, dir = a, d
		work(d)
		return []byte(`{"result":"ok","structured_output":{"ok":true},"total_cost_usd":0.01}`), nil
	}}
	req := RunRequest{Prompt: "fix it", Schema: json.RawMessage(`{"type":"object"}`),
		Workspace: &Workspace{Prepare: repo}, Writes: &WriteLimit{MaxFiles: 10, MaxBytes: 1 << 20}}
	res, err := c.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got []string
	for _, f := range res.Files {
		entry := f.Name + "=" + strings.TrimSpace(string(f.Data))
		if f.Deleted {
			entry = f.Name + " deleted"
		}
		got = append(got, entry)
	}
	sort.Strings(got)
	want := ".gitignore=docs/|docs/new.md=# New|main.go=package main // fixed|old.go deleted"
	if strings.Join(got, "|") != want {
		t.Errorf("changes = %q\n     want %q", strings.Join(got, "|"), want)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--tools Read,Write,Edit ") || !strings.Contains(joined, "Edit(/"+dir+"/**)") {
		t.Errorf("the agent may not change its directory: %s", joined)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the working directory %q was left behind", dir)
	}

	// Limits apply to what changed, not to what was there.
	req.Writes = &WriteLimit{MaxFiles: 3, MaxBytes: 1 << 20}
	if _, err := c.Run(context.Background(), req); err == nil || !strings.Contains(err.Error(), "more than 3 files") {
		t.Errorf("four changes with a limit of three = %v", err)
	}
	// A link is not a file, and is not kept.
	req.Writes = &WriteLimit{MaxFiles: 10, MaxBytes: 1 << 20}
	work = func(d string) { _ = os.Symlink("/etc/hosts", filepath.Join(d, "hosts")) }
	if _, err := c.Run(context.Background(), req); err == nil || !strings.Contains(err.Error(), "other than an ordinary file") {
		t.Errorf("a new link = %v, want it refused", err)
	}
	// Nothing changed is not an error.
	work = func(string) {}
	if res, err := c.Run(context.Background(), req); err != nil || len(res.Files) != 0 {
		t.Errorf("no changes = %v, %v", res.Files, err)
	}
	// A prepared directory and given files do not mix.
	req.Files = []File{{Name: "a.txt"}}
	if _, err := c.Run(context.Background(), req); err == nil {
		t.Error("files were accepted alongside a prepared directory")
	}
}

// TestAnAgentWorksInAPreparedDirectoryLive has a real agent change a prepared
// directory, with a shell. It runs only when asked.
func TestAnAgentWorksInAPreparedDirectoryLive(t *testing.T) {
	if os.Getenv("WEIR_TEST_CLAUDE") == "" {
		t.Skip("set WEIR_TEST_CLAUDE=1 to make a real call to the Claude command line")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	c := &Claude{Models: map[string]string{"": "haiku"}, Shell: true}
	res, err := c.Run(ctx, RunRequest{
		System: "You make small, exact changes to the files in your working directory.",
		Prompt: "1. Change main.go so that its comment reads '// fixed' instead of nothing.\n" +
			"2. Delete old.go, using the shell.\n" +
			"3. Using the shell, create docs/count.txt holding the number of lines in docs/readme.md and nothing else.\n" +
			"Change nothing else.",
		Schema:    json.RawMessage(`{"type":"object","required":["done"],"properties":{"done":{"type":"boolean"}}}`),
		Workspace: &Workspace{Prepare: repo},
		Writes:    &WriteLimit{MaxFiles: 10, MaxBytes: 1 << 20},
		Shell:     &Shell{},
	})
	t.Logf("cost=$%.4f output=%s", res.Usage.CostUSD, res.Output)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := map[string]File{}
	for _, f := range res.Files {
		t.Logf("  %s deleted=%v %q", f.Name, f.Deleted, f.Data)
		got[f.Name] = f
	}
	if len(got) != 3 || !strings.Contains(string(got["main.go"].Data), "// fixed") || !got["old.go"].Deleted ||
		strings.TrimSpace(string(got["docs/count.txt"].Data)) != "1" {
		t.Errorf("changes = %v, want main.go changed, old.go deleted and docs/count.txt holding 1", res.Files)
	}
}

func TestAPassThatKeepsNothingAndItsEffort(t *testing.T) {
	var args []string
	run := func(_ context.Context, _ string, a, _ []string, _, d string) ([]byte, error) {
		args = a
		_ = os.WriteFile(filepath.Join(d, "main.go"), []byte("package main // scribbled on\n"), 0o600)
		_ = os.WriteFile(filepath.Join(d, "notes.txt"), []byte("x"), 0o600)
		return []byte(`{"result":"ok","structured_output":{"ok":true},"total_cost_usd":0.01}`), nil
	}
	req := RunRequest{Prompt: "assess it", Effort: "high", Schema: json.RawMessage(`{"type":"object"}`),
		Workspace: &Workspace{Prepare: repo, Discard: true}, Writes: &WriteLimit{MaxFiles: 1, MaxBytes: 10}}
	for _, c := range []*Claude{{run: run}, {run: run, Unrestricted: true}} {
		res, err := c.Run(context.Background(), req)
		if err != nil {
			t.Fatalf("Run (unrestricted %v): %v", c.Unrestricted, err)
		}
		// It changed two files, against a limit of one. Neither is kept, and
		// the limit is not what stopped them.
		if len(res.Files) != 0 {
			t.Errorf("a pass that keeps nothing kept %v", res.Files)
		}
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--effort high") {
			t.Errorf("the effort was not passed on: %s", joined)
		}
		// An agent in its own container loads its image's skills and settings;
		// one beside the worker loads none.
		want := `--setting-sources  `
		if c.Unrestricted {
			want = "--setting-sources user"
		}
		if !strings.Contains(joined+" ", want) {
			t.Errorf("unrestricted %v: args = %s", c.Unrestricted, joined)
		}
	}
}
