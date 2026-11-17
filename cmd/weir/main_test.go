package main

import (
	"bytes"
	"context"
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
