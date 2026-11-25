// Package config reads Weir's settings from the environment.
package config

import (
	"fmt"
	"math"
	"strconv"
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
	// DatabaseIAM names the cloud whose identity signs in to the database in
	// place of a password. It is "aws" or empty. With it, DatabaseURL holds
	// no password: one is made for each connection.
	DatabaseIAM string
	// AWSRegion is the region a database sign-in token is signed for.
	AWSRegion string
	// HTTPAddr is the address the API listens on.
	HTTPAddr string
	// Runner selects how ai and decide steps run.
	Runner Runner
	// CodexUSDPerMTokIn and CodexUSDPerMTokOut are what a million tokens in
	// and out cost on Codex, in US dollars. Codex reports tokens and no cost,
	// so these are the only way a Codex call can be held to a budget. Zero
	// means no price was given.
	CodexUSDPerMTokIn  float64
	CodexUSDPerMTokOut float64
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
//	WEIR_DATABASE_IAM  optional, "aws": sign in to Amazon RDS with an IAM
//	                   token made for each connection, and no password. The
//	                   database is then given in parts, and the region comes
//	                   from AWS_REGION
//	WEIR_DATABASE_SSLROOTCERT
//	                   optional, with the database in parts: a file of
//	                   certificate authorities to check its certificate against
//	WEIR_HTTP_ADDR     optional, default ":8080", or the port in PORT if a
//	                   platform sets that
//	WEIR_RUNNER        optional, one of cli, api or fake; default "cli"
//	WEIR_CODEX_USD_PER_MTOK_IN, WEIR_CODEX_USD_PER_MTOK_OUT
//	                   optional, the price in US dollars of a million tokens
//	                   in and out on Codex; without both, Codex refuses a
//	                   step that has a budget
func FromEnv(getenv func(string) string) (Config, error) {
	cfg := Config{
		DatabaseURL: strings.TrimSpace(getenv("WEIR_DATABASE_URL")),
		HTTPAddr:    strings.TrimSpace(getenv("WEIR_HTTP_ADDR")),
		Runner:      Runner(strings.ToLower(strings.TrimSpace(getenv("WEIR_RUNNER")))),
	}
	cfg.DatabaseIAM = strings.ToLower(strings.TrimSpace(getenv("WEIR_DATABASE_IAM")))
	switch cfg.DatabaseIAM {
	case "":
		if cfg.DatabaseURL == "" {
			cfg.DatabaseURL = databaseFromParts(getenv)
		}
	case "aws":
		var err error
		if cfg.DatabaseURL, cfg.AWSRegion, err = databaseWithIAM(getenv, cfg.DatabaseURL); err != nil {
			return Config{}, err
		}
	default:
		return Config{}, fmt.Errorf("WEIR_DATABASE_IAM is %q; the only value is aws", cfg.DatabaseIAM)
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
	var err error
	if cfg.CodexUSDPerMTokIn, err = price(getenv, "WEIR_CODEX_USD_PER_MTOK_IN"); err != nil {
		return Config{}, err
	}
	if cfg.CodexUSDPerMTokOut, err = price(getenv, "WEIR_CODEX_USD_PER_MTOK_OUT"); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// price reads a price in US dollars. Unset is zero, which means no price. A
// wrong price would make a budget stop a step too late or too early, so
// anything that is not a plain number of dollars is refused, not guessed at.
func price(getenv func(string) string, name string) (float64, error) {
	raw := strings.TrimSpace(getenv(name))
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, fmt.Errorf("%s is %q; it must be a price in US dollars for a million tokens, such as 1.25, and not less than zero", name, raw)
	}
	return v, nil
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
		{"sslrootcert", "WEIR_DATABASE_SSLROOTCERT"},
	} {
		if v := getenv(p.env); v != "" {
			parts = append(parts, p.key+"="+quote(v))
		}
	}
	return strings.Join(parts, " ")
}

// databaseWithIAM builds the address for a database that is signed in to with
// an IAM token, and returns it with the region the token is signed for. The
// address holds no password. It refuses any setting that contradicts IAM
// sign-in, because a half-applied setting here fails later and less clearly:
// at the first connection, as a refused password.
func databaseWithIAM(getenv func(string) string, url string) (address, region string, err error) {
	set := func(name string) string { return strings.TrimSpace(getenv(name)) }
	const with = "WEIR_DATABASE_IAM is aws"
	host := set("WEIR_DATABASE_HOST")
	switch {
	case url != "":
		return "", "", fmt.Errorf("%s, so the database must be given in parts: unset WEIR_DATABASE_URL, and set WEIR_DATABASE_HOST, WEIR_DATABASE_NAME and WEIR_DATABASE_USER", with)
	case getenv("WEIR_DATABASE_PASSWORD") != "":
		return "", "", fmt.Errorf("%s, so there is no password: unset WEIR_DATABASE_PASSWORD", with)
	case host == "":
		return "", "", fmt.Errorf("%s, and WEIR_DATABASE_HOST is not set: a token is made for one host", with)
	case strings.HasPrefix(host, "/"):
		return "", "", fmt.Errorf("%s, and WEIR_DATABASE_HOST is a socket (%s): a token is made for a host name and a port", with, host)
	case set("WEIR_DATABASE_USER") == "":
		return "", "", fmt.Errorf("%s, and WEIR_DATABASE_USER is not set: a token is made for one database user", with)
	}
	// The token is sent as the password. Without checking the database's
	// certificate, anything that answers on that address would be handed it.
	if mode := set("WEIR_DATABASE_SSLMODE"); mode != "" && mode != "verify-full" {
		return "", "", fmt.Errorf("%s, and WEIR_DATABASE_SSLMODE is %q; it must be verify-full, because the token is sent as the password", with, mode)
	}
	if region = set("AWS_REGION"); region == "" {
		region = set("AWS_DEFAULT_REGION")
	}
	if region == "" {
		return "", "", fmt.Errorf("%s, and AWS_REGION is not set: a token is signed for one region", with)
	}
	address = databaseFromParts(func(name string) string {
		if name == "WEIR_DATABASE_SSLMODE" {
			return "verify-full"
		}
		return getenv(name)
	})
	return address, region, nil
}
