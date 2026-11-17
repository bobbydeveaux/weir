package spec

import (
	"strings"
	"testing"
)

func TestWhichPathsAnAgentMayChange(t *testing.T) {
	no := false
	tests := []struct {
		name   string
		writes *Writes
		paths  []string
		want   string // part of the refusal, or "" when allowed
	}{
		{"nothing set, ordinary files", nil, []string{"main.go", "docs/readme.md"}, ""},
		{"automation is protected by default", nil, []string{"main.go", ".github/workflows/ci.yml"}, ".github/workflows/ci.yml is protected"},
		{"other files under .github are not", &Writes{}, []string{".github/CODEOWNERS"}, ""},
		{"agent instructions are protected", &Writes{}, []string{"CLAUDE.md"}, "CLAUDE.md is protected"},
		{"at any depth", &Writes{}, []string{"services/api/CLAUDE.md"}, "is protected"},
		{"agent settings are protected", &Writes{}, []string{".claude/settings.json"}, "is protected"},
		{"a repository's records are protected", &Writes{}, []string{".git/hooks/pre-commit"}, "is protected"},
		{"a nested one too", &Writes{}, []string{"vendor/lib/.git/config"}, "is protected"},
		{"a step can lift the protection", &Writes{Protected: &no}, []string{".github/workflows/ci.yml", "CLAUDE.md"}, ""},
		{"never", &Writes{Never: []string{"billing/"}}, []string{"billing/invoice.go"}, "may never change"},
		{"never, elsewhere", &Writes{Never: []string{"billing/"}}, []string{"shipping/label.go"}, ""},
		{"only tests", &Writes{Only: []string{"*_test.go", "testdata/"}}, []string{"api/server_test.go", "testdata/a/b.json"}, ""},
		{"only tests, but source changed", &Writes{Only: []string{"*_test.go"}}, []string{"api/server_test.go", "api/server.go"}, "api/server.go is not one of the paths"},
		{"never wins over only", &Writes{Only: []string{"*.go"}, Never: []string{"gen/"}}, []string{"gen/types.go"}, "may never change"},
		{"protected wins over only", &Writes{Only: []string{"*.yml"}}, []string{".github/workflows/ci.yml"}, "is protected"},
		{"nothing changed", &Writes{Only: []string{"*_test.go"}}, nil, ""},
	}
	for _, tt := range tests {
		got := tt.writes.Refuse(tt.paths)
		if (tt.want == "") != (got == "") || !strings.Contains(got, tt.want) {
			t.Errorf("%s: Refuse(%v) = %q, want %q", tt.name, tt.paths, got, tt.want)
		}
	}
	if rules := (&Writes{Only: []string{"*_test.go"}}).Rules(); !strings.Contains(rules, "*_test.go") || !strings.Contains(rules, "CLAUDE.md") {
		t.Errorf("Rules = %q", rules)
	}
	if rules := (&Writes{Protected: &no}).Rules(); rules != "" {
		t.Errorf("Rules with nothing in force = %q", rules)
	}
}
