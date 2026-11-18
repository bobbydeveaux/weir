package config

import (
	"strings"
	"testing"
)

func env(values map[string]string) func(string) string {
	return func(key string) string { return values[key] }
}

func TestFromEnvDefaults(t *testing.T) {
	cfg, err := FromEnv(env(map[string]string{"WEIR_DATABASE_URL": "postgres://localhost/weir"}))
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.Runner != RunnerCLI {
		t.Errorf("Runner = %q, want the command-line runner by default", cfg.Runner)
	}
	if cfg.HTTPAddr != ":8080" {
		t.Errorf("HTTPAddr = %q, want :8080", cfg.HTTPAddr)
	}
}

func TestFromEnvRunner(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  Runner
	}{
		{"cli", RunnerCLI},
		{"api", RunnerAPI},
		{"fake", RunnerFake},
		{" API ", RunnerAPI},
	} {
		cfg, err := FromEnv(env(map[string]string{
			"WEIR_DATABASE_URL": "postgres://localhost/weir",
			"WEIR_RUNNER":       tc.value,
		}))
		if err != nil {
			t.Errorf("WEIR_RUNNER=%q: %v", tc.value, err)
			continue
		}
		if cfg.Runner != tc.want {
			t.Errorf("WEIR_RUNNER=%q: Runner = %q, want %q", tc.value, cfg.Runner, tc.want)
		}
	}
}

func TestFromEnvRejectsUnknownRunner(t *testing.T) {
	_, err := FromEnv(env(map[string]string{
		"WEIR_DATABASE_URL": "postgres://localhost/weir",
		"WEIR_RUNNER":       "magic",
	}))
	if err == nil || !strings.Contains(err.Error(), "magic") {
		t.Fatalf("err = %v, want an error naming the bad value", err)
	}
}

func TestFromEnvRequiresDatabase(t *testing.T) {
	if _, err := FromEnv(env(nil)); err == nil {
		t.Fatal("FromEnv succeeded with no database URL")
	}
}

func TestTheDatabaseCanBeGivenInParts(t *testing.T) {
	cfg, err := FromEnv(env(map[string]string{
		"WEIR_DATABASE_HOST": "/cloudsql/project:region:instance", "WEIR_DATABASE_NAME": "weir",
		"WEIR_DATABASE_USER": "weir", "WEIR_DATABASE_PASSWORD": `p'a ss\word`, "PORT": "9000",
	}))
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	want := `host='/cloudsql/project:region:instance' dbname='weir' user='weir' password='p\'a ss\\word'`
	if cfg.DatabaseURL != want {
		t.Errorf("DatabaseURL = %s\n         want %s", cfg.DatabaseURL, want)
	}
	if cfg.HTTPAddr != ":9000" {
		t.Errorf("HTTPAddr = %q, want the port the platform named", cfg.HTTPAddr)
	}
	// One address wins over the parts, and an address of its own wins over PORT.
	cfg, err = FromEnv(env(map[string]string{"WEIR_DATABASE_URL": "postgres://x/y", "WEIR_DATABASE_HOST": "h", "PORT": "9000", "WEIR_HTTP_ADDR": ":7000"}))
	if err != nil || cfg.DatabaseURL != "postgres://x/y" || cfg.HTTPAddr != ":7000" {
		t.Errorf("cfg = %+v, %v", cfg, err)
	}
	if _, err := FromEnv(env(nil)); err == nil || !strings.Contains(err.Error(), "WEIR_DATABASE_HOST") {
		t.Errorf("with no database at all = %v", err)
	}
}
