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

func TestCodexPricesAreReadFromSettings(t *testing.T) {
	cfg, err := FromEnv(env(map[string]string{
		"WEIR_DATABASE_URL":           "postgres://localhost/weir",
		"WEIR_CODEX_USD_PER_MTOK_IN":  " 1.25 ",
		"WEIR_CODEX_USD_PER_MTOK_OUT": "10",
	}))
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.CodexUSDPerMTokIn != 1.25 || cfg.CodexUSDPerMTokOut != 10 {
		t.Errorf("prices = %v in, %v out; want 1.25 and 10", cfg.CodexUSDPerMTokIn, cfg.CodexUSDPerMTokOut)
	}
}

func TestCodexHasNoPricesUnlessTheyAreSet(t *testing.T) {
	cfg, err := FromEnv(env(map[string]string{"WEIR_DATABASE_URL": "postgres://localhost/weir"}))
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if cfg.CodexUSDPerMTokIn != 0 || cfg.CodexUSDPerMTokOut != 0 {
		t.Errorf("prices = %v in, %v out; want none", cfg.CodexUSDPerMTokIn, cfg.CodexUSDPerMTokOut)
	}
}

func TestABadCodexPriceIsRefusedByName(t *testing.T) {
	for _, name := range []string{"WEIR_CODEX_USD_PER_MTOK_IN", "WEIR_CODEX_USD_PER_MTOK_OUT"} {
		for _, bad := range []string{"cheap", "-1", "NaN", "Inf", "$2"} {
			_, err := FromEnv(env(map[string]string{"WEIR_DATABASE_URL": "postgres://localhost/weir", name: bad}))
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("%s=%q: err = %v, want an error naming the setting", name, bad, err)
			}
		}
	}
}

func TestTheCertificateAuthoritiesForTheDatabaseCanBeNamed(t *testing.T) {
	cfg, err := FromEnv(env(map[string]string{
		"WEIR_DATABASE_HOST": "db.example.com", "WEIR_DATABASE_USER": "weir", "WEIR_DATABASE_PASSWORD": "pw",
		"WEIR_DATABASE_SSLMODE": "verify-full", "WEIR_DATABASE_SSLROOTCERT": "/etc/weir/db-ca/ca.pem",
	}))
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	if !strings.Contains(cfg.DatabaseURL, "sslrootcert='/etc/weir/db-ca/ca.pem'") {
		t.Errorf("DatabaseURL = %s; want it to name the file of certificate authorities", cfg.DatabaseURL)
	}
}

func TestTheCertificateAuthoritiesAreRefusedBesideOneAddress(t *testing.T) {
	_, err := FromEnv(env(map[string]string{
		"WEIR_DATABASE_URL": "postgres://db.example.com/weir?sslmode=verify-full", "WEIR_DATABASE_SSLROOTCERT": "/etc/weir/db-ca/ca.pem",
	}))
	if err == nil {
		t.Fatal("FromEnv accepted WEIR_DATABASE_SSLROOTCERT with WEIR_DATABASE_URL, which ignores it")
	}
	for _, want := range []string{"WEIR_DATABASE_SSLROOTCERT", "sslrootcert=", "in parts"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("err = %v; want it to say %q", err, want)
		}
	}
	// One address that carries its own file is accepted as it is.
	url := "postgres://db.example.com/weir?sslmode=verify-full&sslrootcert=/etc/weir/db-ca/ca.pem"
	cfg, err := FromEnv(env(map[string]string{"WEIR_DATABASE_URL": url}))
	if err != nil || cfg.DatabaseURL != url {
		t.Errorf("cfg = %+v, %v; want the address unchanged", cfg, err)
	}
}

// iam is the least that IAM sign-in needs, for the tests to add to or take from.
func iam(change map[string]string) map[string]string {
	settings := map[string]string{
		"WEIR_DATABASE_IAM": "aws", "WEIR_DATABASE_HOST": "db.example.com", "WEIR_DATABASE_NAME": "weir",
		"WEIR_DATABASE_USER": "weir", "AWS_REGION": "eu-west-2",
	}
	for k, v := range change {
		if v == "" {
			delete(settings, k)
		} else {
			settings[k] = v
		}
	}
	return settings
}

func TestIAMSignInBuildsAnAddressWithNoPassword(t *testing.T) {
	cfg, err := FromEnv(env(iam(map[string]string{"WEIR_DATABASE_PORT": "5433", "WEIR_DATABASE_SSLROOTCERT": "/ca.pem"})))
	if err != nil {
		t.Fatalf("FromEnv: %v", err)
	}
	want := `host='db.example.com' port='5433' dbname='weir' user='weir' sslmode='verify-full' sslrootcert='/ca.pem'`
	if cfg.DatabaseURL != want {
		t.Errorf("DatabaseURL = %s\n         want %s", cfg.DatabaseURL, want)
	}
	if cfg.DatabaseIAM != "aws" || cfg.AWSRegion != "eu-west-2" {
		t.Errorf("DatabaseIAM = %q, AWSRegion = %q; want aws and eu-west-2", cfg.DatabaseIAM, cfg.AWSRegion)
	}
	// The mode may be said outright, and the region may come from the older setting.
	cfg, err = FromEnv(env(iam(map[string]string{"WEIR_DATABASE_SSLMODE": "verify-full", "AWS_REGION": "", "AWS_DEFAULT_REGION": "us-east-1", "WEIR_DATABASE_IAM": " AWS "})))
	if err != nil || cfg.AWSRegion != "us-east-1" || !strings.Contains(cfg.DatabaseURL, "sslmode='verify-full'") {
		t.Errorf("cfg = %+v, %v", cfg, err)
	}
	// Without the setting, nothing about IAM applies.
	cfg, err = FromEnv(env(map[string]string{"WEIR_DATABASE_URL": "postgres://localhost/weir"}))
	if err != nil || cfg.DatabaseIAM != "" {
		t.Errorf("with no WEIR_DATABASE_IAM, DatabaseIAM = %q, %v", cfg.DatabaseIAM, err)
	}
}

func TestIAMSignInRefusesASettingThatContradictsIt(t *testing.T) {
	for name, tc := range map[string]struct {
		change map[string]string
		names  string // what the error must name
	}{
		"one address as well":       {map[string]string{"WEIR_DATABASE_URL": "postgres://db.example.com/weir"}, "WEIR_DATABASE_URL"},
		"a password as well":        {map[string]string{"WEIR_DATABASE_PASSWORD": "pw"}, "WEIR_DATABASE_PASSWORD"},
		"no host":                   {map[string]string{"WEIR_DATABASE_HOST": ""}, "WEIR_DATABASE_HOST"},
		"a socket":                  {map[string]string{"WEIR_DATABASE_HOST": "/cloudsql/p:r:i"}, "socket"},
		"a list of hosts":           {map[string]string{"WEIR_DATABASE_HOST": "db-a.example.com,db-b.example.com"}, "WEIR_DATABASE_HOST is a list"},
		"a list of ports":           {map[string]string{"WEIR_DATABASE_PORT": "5432,5433"}, "WEIR_DATABASE_PORT is a list"},
		"no user":                   {map[string]string{"WEIR_DATABASE_USER": ""}, "WEIR_DATABASE_USER"},
		"no region":                 {map[string]string{"AWS_REGION": ""}, "AWS_REGION"},
		"a certificate not named":   {map[string]string{"WEIR_DATABASE_SSLMODE": "verify-ca"}, "verify-full"},
		"a certificate not checked": {map[string]string{"WEIR_DATABASE_SSLMODE": "require"}, "verify-full"},
		"no encryption":             {map[string]string{"WEIR_DATABASE_SSLMODE": "disable"}, "verify-full"},
		"another cloud":             {map[string]string{"WEIR_DATABASE_IAM": "gcp"}, "the only value is aws"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := FromEnv(env(iam(tc.change)))
			if err == nil || !strings.Contains(err.Error(), tc.names) {
				t.Errorf("err = %v; want a refusal that names %s", err, tc.names)
			}
		})
	}
}
