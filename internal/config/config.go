// Package config reads Weir's settings from the environment.
package config

import (
	"fmt"
	"strings"
)

// Runner names how ai and decide steps are executed.
type Runner string

const (
	// RunnerCLI runs steps through the Claude Code command line on a long-lived OAuth token.
	RunnerCLI Runner = "cli"
	// RunnerAPI calls a model provider's API directly with an API key.
	RunnerAPI Runner = "api"
	// RunnerFake returns scripted answers. It is for tests and the examples.
	RunnerFake Runner = "fake"
)

// Config is everything the engine needs to start.
type Config struct {
	// DatabaseURL is the Postgres connection string.
	DatabaseURL string
	// HTTPAddr is the address the API listens on.
	HTTPAddr string
	// Runner selects how ai and decide steps run.
	Runner Runner
}

const (
	defaultHTTPAddr = ":8080"
	defaultRunner   = RunnerCLI
)

// FromEnv builds a Config from environment variables, read through getenv so
// that tests can supply their own values.
//
//	WEIR_DATABASE_URL  the database, as one address; or, for a platform that
//	                   hands out the parts, WEIR_DATABASE_HOST with _PORT,
//	                   _NAME, _USER and _PASSWORD. One of the two is required
//	WEIR_HTTP_ADDR     optional, default ":8080", or the port in PORT if a
//	                   platform sets that
//	WEIR_RUNNER        optional, one of cli, api or fake; default "cli"
func FromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL: strings.TrimSpace(getenv("WEIR_DATABASE_URL")),
		HTTPAddr:    strings.TrimSpace(getenv("WEIR_HTTP_ADDR")),
		Runner:      Runner(strings.ToLower(strings.TrimSpace(getenv("WEIR_RUNNER")))),
	}
	if cfg.DatabaseURL == "" {
		cfg.DatabaseURL = databaseFromParts(getenv)
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("WEIR_DATABASE_URL is not set, and neither is WEIR_DATABASE_HOST")
	}
	if port := strings.TrimSpace(getenv("PORT")); cfg.HTTPAddr == "" && port != "" {
		cfg.HTTPAddr = ":" + port // Cloud Run and others say which port to listen on
	}
	if cfg.HTTPAddr == "" {
		cfg.HTTPAddr = defaultHTTPAddr
	}
	if cfg.Runner == "" {
		cfg.Runner = defaultRunner
	}
	switch cfg.Runner {
	case RunnerCLI, RunnerAPI, RunnerFake:
	default:
		return Config{}, fmt.Errorf("WEIR_RUNNER is %q; it must be cli, api or fake", cfg.Runner)
	}
	return cfg, nil
}

// databaseFromParts builds a connection string from separate settings, for a
// platform that gives the host, user and password one at a time. The host may
// be a directory holding a socket, as Cloud SQL's is. It returns "" when no
// host is set.
func databaseFromParts(getenv func(string) string) string {
	host := strings.TrimSpace(getenv("WEIR_DATABASE_HOST"))
	if host == "" {
		return ""
	}
	// Each value is quoted, so that a password may hold any character.
	quote := func(v string) string {
		return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(v) + "'"
	}
	parts := []string{"host=" + quote(host)}
	for _, p := range []struct{ key, env string }{
		{"port", "WEIR_DATABASE_PORT"}, {"dbname", "WEIR_DATABASE_NAME"},
		{"user", "WEIR_DATABASE_USER"}, {"password", "WEIR_DATABASE_PASSWORD"}, {"sslmode", "WEIR_DATABASE_SSLMODE"},
	} {
		if v := getenv(p.env); v != "" {
			parts = append(parts, p.key+"="+quote(v))
		}
	}
	return strings.Join(parts, " ")
}
