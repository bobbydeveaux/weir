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
