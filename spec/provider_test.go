package spec

import (
	"strings"
	"testing"
)

func decideWith(provider string) string {
	return `
weir: "0.1"
namespace: t
name: p
steps:
  - id: classify
    kind: decide
    template: c@1
    labels: [a, b]
    default: a
` + provider
}

func TestADecideStepMayNameItsProvider(t *testing.T) {
	for _, name := range DecideProviders {
		d, err := Parse([]byte(decideWith("    provider: " + name + "\n")))
		if err != nil {
			t.Fatalf("%s: parse: %v", name, err)
		}
		if err := d.Validate(ValidateOptions{}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if d.Steps[0].Provider != name {
			t.Errorf("provider = %q, want %q", d.Steps[0].Provider, name)
		}
	}
	d, err := Parse([]byte(decideWith("    provider: crystal-ball\n")))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(ValidateOptions{}); err == nil || !strings.Contains(err.Error(), "crystal-ball") {
		t.Errorf("err = %v, want the unknown provider refused", err)
	}
}

func TestDescriptionsMustBelongToLabels(t *testing.T) {
	d, err := Parse([]byte(decideWith("    describe: { a: The first, b: The second }\n")))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(ValidateOptions{}); err != nil {
		t.Errorf("valid descriptions were refused: %v", err)
	}
	d, err = Parse([]byte(decideWith("    describe: { c: Not a label }\n")))
	if err != nil {
		t.Fatal(err)
	}
	if err := d.Validate(ValidateOptions{}); err == nil || !strings.Contains(err.Error(), `"c"`) {
		t.Errorf("err = %v, want the stray description refused", err)
	}
}

// A person may approve with or without sending corrected values; a later step
// must be able to fall back when they sent none.
func TestAnExpressionCanFallBackWhenADecisionCarriesNoData(t *testing.T) {
	const expr = `steps.approve.output.data != null && has(steps.approve.output.data.terms) ? steps.approve.output.data.terms : steps.validate.output.proposed`
	proposed := map[string]any{"validate": map[string]any{"output": map[string]any{"proposed": "from-agent"}}}
	for name, tc := range map[string]struct {
		data any
		want string
	}{
		"no data":      {nil, "from-agent"},
		"other data":   {map[string]any{"edited": []any{}}, "from-agent"},
		"edited terms": {map[string]any{"terms": "from-person"}, "from-person"},
	} {
		steps := map[string]any{"validate": proposed["validate"],
			"approve": map[string]any{"output": map[string]any{"outcome": "approved", "data": tc.data}}}
		got, err := Eval(expr, Scope{Steps: steps})
		if err != nil || got != tc.want {
			t.Errorf("%s: got %v, %v; want %q", name, got, err, tc.want)
		}
	}
}
