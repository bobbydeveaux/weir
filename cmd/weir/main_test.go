package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func noEnv(string) string { return "" }

func TestVersion(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"version"}, noEnv, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %s)", code, errOut.String())
	}
	if strings.TrimSpace(out.String()) != version {
		t.Fatalf("printed %q, want %q", out.String(), version)
	}
}

func TestNoCommandPrintsUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), nil, noEnv, &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "Usage:") {
		t.Fatalf("stderr = %q, want usage", errOut.String())
	}
}

func TestUnknownCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"dance"}, noEnv, &out, &errOut); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), `"dance"`) {
		t.Fatalf("stderr = %q, want it to name the command", errOut.String())
	}
}

func TestMigrateNeedsADatabase(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"migrate"}, noEnv, &out, &errOut); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "WEIR_DATABASE_URL") {
		t.Fatalf("stderr = %q, want it to name the missing setting", errOut.String())
	}
}

func TestCommandsCheckTheirArguments(t *testing.T) {
	for _, args := range [][]string{
		{"publish"},
		{"run"},
		{"status"},
		{"approve"},
		{"connection", "only-a-name"},
	} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), args, noEnv, &out, &errOut); code != 1 {
			t.Errorf("%v: exit code = %d, want 1", args, code)
		}
		if !strings.Contains(errOut.String(), "argument") {
			t.Errorf("%v: stderr = %q, want it to explain the arguments", args, errOut.String())
		}
	}
}

func TestHelpListsEveryCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"help"}, noEnv, &out, &errOut); code != 0 {
		t.Fatalf("exit code = %d", code)
	}
	for _, name := range []string{"migrate", "serve", "publish", "template", "connection", "run", "status", "tasks", "approve", "resume"} {
		if !strings.Contains(out.String(), name) {
			t.Errorf("help does not mention %s", name)
		}
	}
}

func TestListsAndPairsAreReadFromSettings(t *testing.T) {
	if got := names(" A, ,B ,"); len(got) != 2 || got[0] != "A" || got[1] != "B" {
		t.Errorf("names = %q", got)
	}
	got := pairs("small=claude-haiku-5-5, large = claude-opus-5-5, broken, =x, y=")
	if len(got) != 2 || got["small"] != "claude-haiku-5-5" || got["large"] != "claude-opus-5-5" {
		t.Errorf("pairs = %v", got)
	}
}

func TestWhetherAServerDoesTheWork(t *testing.T) {
	for setting, want := range map[string]bool{"": true, "on": true, "off": false} {
		got, err := works(setting)
		if err != nil || got != want {
			t.Errorf("works(%q) = %v, %v; want %v", setting, got, err, want)
		}
	}
	if _, err := works("sometimes"); err == nil {
		t.Error("a setting that is neither on nor off was accepted")
	}
}

func TestHowManyStepsAServerRunsAtOnce(t *testing.T) {
	for setting, want := range map[string]int{"": 1, "1": 1, " 8 ": 8, "256": 256} {
		if got, err := concurrency(setting); err != nil || got != want {
			t.Errorf("concurrency(%q) = %d, %v; want %d", setting, got, err, want)
		}
	}
	for _, setting := range []string{"0", "-1", "two", "1.5", "257", "1000"} {
		if _, err := concurrency(setting); err == nil || !strings.Contains(err.Error(), "WEIR_WORKER_CONCURRENCY") {
			t.Errorf("concurrency(%q) = %v; want it refused, naming WEIR_WORKER_CONCURRENCY", setting, err)
		}
	}
}

// settings answers getenv from a map, as a server's environment would.
func settings(env map[string]string) func(string) string {
	return func(name string) string { return env[name] }
}

// keyFile writes contents to a file of its own and returns its path.
func keyFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "admin-key")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestTheAdminKeyCanBeReadFromAFile(t *testing.T) {
	key, err := readAdminKey(settings(map[string]string{"WEIR_ADMIN_KEY_FILE": keyFile(t, "  s3cret-from-a-file\n")}))
	if err != nil || key != "s3cret-from-a-file" {
		t.Fatalf("readAdminKey = %q, %v; want the file's contents without the whitespace", key, err)
	}
	key, err = readAdminKey(settings(map[string]string{"WEIR_ADMIN_KEY": "s3cret-from-the-env"}))
	if err != nil || key != "s3cret-from-the-env" {
		t.Fatalf("readAdminKey = %q, %v; want WEIR_ADMIN_KEY", key, err)
	}
	key, err = readAdminKey(noEnv)
	if err != nil || key != "" {
		t.Fatalf("readAdminKey with no settings = %q, %v; want no key and no error", key, err)
	}
}

func TestTheAdminKeySettingsAreRefusedWhenWrong(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-file")
	for name, tc := range map[string]struct {
		env      map[string]string
		want     []string
		mustHide []string
	}{
		"both settings": {
			env:      map[string]string{"WEIR_ADMIN_KEY": "s3cret-in-env", "WEIR_ADMIN_KEY_FILE": keyFile(t, "s3cret-in-file\n")},
			want:     []string{"WEIR_ADMIN_KEY ", "WEIR_ADMIN_KEY_FILE", "only one"},
			mustHide: []string{"s3cret-in-env", "s3cret-in-file"},
		},
		"a missing file": {
			env:      map[string]string{"WEIR_ADMIN_KEY_FILE": missing},
			want:     []string{"WEIR_ADMIN_KEY_FILE", "could not be read"},
			mustHide: []string{missing},
		},
		"the key put where the path should be": {
			env:      map[string]string{"WEIR_ADMIN_KEY_FILE": "s3cret-not-a-path"},
			want:     []string{"WEIR_ADMIN_KEY_FILE", "could not be read"},
			mustHide: []string{"s3cret-not-a-path"},
		},
		"an empty file": {
			env:  map[string]string{"WEIR_ADMIN_KEY_FILE": keyFile(t, " \n")},
			want: []string{"WEIR_ADMIN_KEY_FILE", "empty"},
		},
		"required and not given": {
			env:  map[string]string{"WEIR_REQUIRE_ADMIN_KEY": "1"},
			want: []string{"WEIR_ADMIN_KEY ", "WEIR_ADMIN_KEY_FILE", "WEIR_REQUIRE_ADMIN_KEY"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			key, err := readAdminKey(settings(tc.env))
			if err == nil {
				t.Fatalf("readAdminKey = %q and no error; want it refused", key)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error()+" ", want) {
					t.Errorf("error %q does not name %q", err, want)
				}
			}
			for _, hidden := range tc.mustHide {
				if strings.Contains(err.Error(), hidden) {
					t.Errorf("error %q repeats %q", err, hidden)
				}
			}
		})
	}
}

func TestEitherSettingMeetsTheNeedForAnAdminKey(t *testing.T) {
	for name, env := range map[string]map[string]string{
		"WEIR_ADMIN_KEY":      {"WEIR_REQUIRE_ADMIN_KEY": "1", "WEIR_ADMIN_KEY": "s3cret"},
		"WEIR_ADMIN_KEY_FILE": {"WEIR_REQUIRE_ADMIN_KEY": "1", "WEIR_ADMIN_KEY_FILE": keyFile(t, "s3cret\n")},
	} {
		if key, err := readAdminKey(settings(env)); err != nil || key != "s3cret" {
			t.Errorf("with %s: readAdminKey = %q, %v; want the key", name, key, err)
		}
	}
}

// weir serve checks the admin key before it opens the database, so these run
// with no database: a refusal names the admin key setting, and a setting that
// is accepted gets as far as asking for a database.
func TestServeChecksTheAdminKeyBeforeAnythingElse(t *testing.T) {
	for name, tc := range map[string]struct {
		env  map[string]string
		want string
	}{
		"no key, and one is required": {map[string]string{"WEIR_REQUIRE_ADMIN_KEY": "1"}, "WEIR_REQUIRE_ADMIN_KEY"},
		"both settings":               {map[string]string{"WEIR_ADMIN_KEY": "s3cret", "WEIR_ADMIN_KEY_FILE": keyFile(t, "s3cret")}, "only one"},
		"a missing file":              {map[string]string{"WEIR_ADMIN_KEY_FILE": filepath.Join(t.TempDir(), "gone")}, "WEIR_ADMIN_KEY_FILE"},
		"an empty file":               {map[string]string{"WEIR_ADMIN_KEY_FILE": keyFile(t, "")}, "empty"},
		"the key in a file, and one is required": {
			map[string]string{"WEIR_REQUIRE_ADMIN_KEY": "1", "WEIR_ADMIN_KEY_FILE": keyFile(t, "s3cret\n")}, "WEIR_DATABASE_URL",
		},
	} {
		var out, errOut bytes.Buffer
		if code := run(context.Background(), []string{"serve"}, settings(tc.env), &out, &errOut); code != 1 {
			t.Errorf("%s: exit code = %d, want 1", name, code)
		}
		if !strings.Contains(errOut.String(), tc.want) {
			t.Errorf("%s: stderr = %q, want it to say %q", name, errOut.String(), tc.want)
		}
		if strings.Contains(errOut.String(), "s3cret") {
			t.Errorf("%s: stderr = %q repeats the key", name, errOut.String())
		}
	}
}
