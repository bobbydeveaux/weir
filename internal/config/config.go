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
//	WEIR_DATABASE_URL  required
//	WEIR_HTTP_ADDR     optional, default ":8080"
//	WEIR_RUNNER        optional, one of cli, api or fake; default "cli"
func FromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL: strings.TrimSpace(getenv("WEIR_DATABASE_URL")),
		HTTPAddr:    strings.TrimSpace(getenv("WEIR_HTTP_ADDR")),
		Runner:      Runner(strings.ToLower(strings.TrimSpace(getenv("WEIR_RUNNER")))),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("WEIR_DATABASE_URL is not set")
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
