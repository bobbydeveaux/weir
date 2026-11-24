package spec

import (
	"strings"
	"testing"
)

const valid = `
weir: "0.1"
namespace: test
name: sample
steps:
  - id: route
    kind: decide
    template: route@1
    labels: [fix, skip]
    confidence: 0.8
    default: skip
    next:
      - when: label == "fix"
        go: work
  - id: work
    kind: deterministic
    handler: core.noop
    input:
      title: input.title
`

func TestParseAndValidate(t *testing.T) {
	def, err := Parse([]byte(valid))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := def.Validate(ValidateOptions{}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := len(def.Steps); got != 2 {
		t.Fatalf("steps = %d, want 2", got)
	}
}

func TestParseRejectsUnknownKeys(t *testing.T) {
	_, err := Parse([]byte(strings.Replace(valid, "handler: core.noop", "handlr: core.noop", 1)))
	if err == nil {
		t.Fatal("a misspelt key was accepted")
	}
}

func TestValidateFindsEveryProblem(t *testing.T) {
	for name, tc := range map[string]struct {
		change func(string) string
		want   string
	}{
		"wrong contract version": {
			func(s string) string { return strings.Replace(s, `weir: "0.1"`, `weir: "9"`, 1) }, "weir: must be",
		},
		"default is not a label": {
			func(s string) string { return strings.Replace(s, "default: skip", "default: other", 1) }, "must be one of the labels",
		},
		"template is not pinned": {
			func(s string) string { return strings.Replace(s, "template: route@1", "template: route", 1) }, "pinned version",
		},
		"next names a missing step": {
			func(s string) string { return strings.Replace(s, "go: work", "go: nowhere", 1) }, "does not exist",
		},
		"expression does not compile": {
			func(s string) string { return strings.Replace(s, `label == "fix"`, `label ==`, 1) }, "next[0].when",
		},
		"field from another kind": {
			func(s string) string {
				return strings.Replace(s, "handler: core.noop", "handler: core.noop\n    prompt: fix@1", 1)
			},
			"prompt is not a field of a deterministic step",
		},
		"unknown kind": {
			func(s string) string { return strings.Replace(s, "kind: deterministic", "kind: magic", 1) }, "is not one of",
		},
	} {
		t.Run(name, func(t *testing.T) {
			def, err := Parse([]byte(tc.change(valid)))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			err = def.Validate(ValidateOptions{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate = %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestValidateChecksHandlersWhenAsked(t *testing.T) {
	def, err := Parse([]byte(valid))
	if err != nil {
		t.Fatal(err)
	}
	err = def.Validate(ValidateOptions{HandlerExists: func(string) bool { return false }})
	if err == nil || !strings.Contains(err.Error(), "is not registered") {
		t.Fatalf("Validate = %v, want an unregistered-handler error", err)
	}
}

func TestAnAIStepNeedsSchemaAndBudget(t *testing.T) {
	def, err := Parse([]byte(`
weir: "0.1"
namespace: test
name: sample
steps:
  - id: fix
    kind: ai
    prompt: fix@1
`))
	if err != nil {
		t.Fatal(err)
	}
	err = def.Validate(ValidateOptions{})
	if err == nil || !strings.Contains(err.Error(), "output schema") || !strings.Contains(err.Error(), "budget") {
		t.Fatalf("Validate = %v, want errors about the schema and the budget", err)
	}
}

func TestResolveEvaluatesStringsAndKeepsTheRest(t *testing.T) {
	scope := Scope{
		Input: map[string]any{"title": "Checkout is down"},
		Steps: map[string]any{"open": map[string]any{"output": map[string]any{"number": 482.0}}},
	}
	got, err := Resolve(map[string]any{
		"title":  "input.title",
		"issue":  "steps.open.output.number",
		"fixed":  `"always this"`,
		"count":  3,
		"labels": []any{`"remediation"`},
	}, scope)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got["title"] != "Checkout is down" || got["issue"] != 482.0 || got["fixed"] != "always this" || got["count"] != 3 {
		t.Fatalf("Resolve = %#v", got)
	}
	if labels, ok := got["labels"].([]any); !ok || len(labels) != 1 || labels[0] != "remediation" {
		t.Fatalf("labels = %#v", got["labels"])
	}
}

func TestParseDuration(t *testing.T) {
	for in, want := range map[string]string{"30s": "30s", "5m": "5m0s", "48h": "48h0m0s", "2d": "48h0m0s"} {
		d, err := ParseDuration(in)
		if err != nil || d.String() != want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %s", in, d, err, want)
		}
	}
	if _, err := ParseDuration("soon"); err == nil {
		t.Error(`ParseDuration("soon") succeeded`)
	}
}

const acceptedNotEnforced = `
weir: "0.1"
namespace: test
name: limits
budget: { usd: 5, minutes: 30 }
tiers:
  fast: { model: small }
steps:
  - id: fix
    kind: ai
    prompt: fix@1
    output: { schema: { type: object } }
    budget: { usd: 2, minutes: 10 }
    tools:
      read: [files.read]
      approve: [git.push]
    sandbox: { network: none }
    hooks: { before: [lint] }
`

func TestUnsupportedNamesWhatIsAcceptedButNotEnforced(t *testing.T) {
	def, err := Parse([]byte(acceptedNotEnforced))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if err := def.Validate(ValidateOptions{}); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	got := def.Unsupported()
	want := []string{"tiers", "budget.minutes", "steps.fix.hooks", "steps.fix.sandbox", "steps.fix.tools.approve"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("Unsupported() = %v, want %v", got, want)
	}
}

func TestUnsupportedLeavesOutABudgetThatIsEnforced(t *testing.T) {
	for name, source := range map[string]string{
		"a run's budget in dollars alone": `
weir: "0.1"
namespace: test
name: dollars
budget: { usd: 5 }
steps:
  - id: work
    kind: deterministic
    handler: core.noop
`,
		// The worker stops an agent that runs longer than its step's minutes.
		"a step's minutes": `
weir: "0.1"
namespace: test
name: step-minutes
budget: { usd: 5 }
steps:
  - id: fix
    kind: ai
    prompt: fix@1
    output: { schema: { type: object } }
    budget: { usd: 2, minutes: 10 }
`,
	} {
		def, err := Parse([]byte(source))
		if err != nil {
			t.Fatalf("%s: Parse: %v", name, err)
		}
		if err := def.Validate(ValidateOptions{}); err != nil {
			t.Fatalf("%s: Validate: %v", name, err)
		}
		if got := def.Unsupported(); len(got) != 0 {
			t.Errorf("%s: Unsupported() = %v, want nothing", name, got)
		}
	}
}
