package runner

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

const claudeSuccess = `{"type":"result","subtype":"success","is_error":false,"result":"done",
"structured_output":{"branch":"fix/482"},"total_cost_usd":0.0123,
"usage":{"input_tokens":10,"cache_read_input_tokens":90,"output_tokens":20},
"modelUsage":{"claude-haiku-5-5":{"inputTokens":10}}}`

func fakeClaude(reply string, err error, seen *[]string, stdin *string) *Claude {
	return &Claude{run: func(_ context.Context, _ string, args, _ []string, in string) ([]byte, error) {
		*seen = args
		*stdin = in
		return []byte(reply), err
	}}
}

func TestClaudeRunParsesOutputCostAndUsage(t *testing.T) {
	var args []string
	var stdin string
	c := fakeClaude(claudeSuccess, nil, &args, &stdin)
	res, err := c.Run(context.Background(), RunRequest{System: "be careful", Prompt: "fix it",
		Schema: json.RawMessage(`{"type":"object"}`), BudgetUSD: 15})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Output) != `{"branch":"fix/482"}` {
		t.Errorf("Output = %s", res.Output)
	}
	if res.Usage.CostUSD != 0.0123 || res.Usage.TokensIn != 100 || res.Usage.TokensOut != 20 {
		t.Errorf("Usage = %+v", res.Usage)
	}
	if res.Usage.Provider != "claude" || res.Usage.Model != "claude-haiku-5-5" {
		t.Errorf("Usage names %s / %s", res.Usage.Provider, res.Usage.Model)
	}
	if stdin != "fix it" {
		t.Errorf("the prompt was not passed on standard input: %q", stdin)
	}
	for _, want := range []string{"--print", "--strict-mcp-config", "--no-session-persistence", "--max-budget-usd", "15.00", "--json-schema"} {
		if !slices.Contains(args, want) {
			t.Errorf("args are missing %s: %v", want, args)
		}
	}
}

func TestClaudeIsNeverRunWithPermissionChecksOff(t *testing.T) {
	args := strings.Join(claudeArgs("system", "opus", []byte(`{}`), 5), " ")
	for _, banned := range []string{"dangerously", "bypassPermissions", "yolo"} {
		if strings.Contains(args, banned) {
			t.Errorf("args contain %q: %s", banned, args)
		}
	}
	if !strings.Contains(args, "--permission-mode dontAsk") {
		t.Errorf("args do not keep permission checks on: %s", args)
	}
	// "--tools" followed by an empty value switches every built-in tool off.
	list := claudeArgs("", "", nil, 0)
	if i := slices.Index(list, "--tools"); i < 0 || list[i+1] != "" {
		t.Errorf("built-in tools are not switched off: %v", list)
	}
}

func TestClaudeRunFailsWithoutStructuredOutput(t *testing.T) {
	var args []string
	var stdin string
	c := fakeClaude(`{"is_error":false,"result":"just text","total_cost_usd":0.01}`, nil, &args, &stdin)
	res, err := c.Run(context.Background(), RunRequest{Prompt: "x", Schema: json.RawMessage(`{}`)})
	if err == nil {
		t.Fatal("Run succeeded with no structured output")
	}
	if res.Usage.CostUSD != 0.01 {
		t.Errorf("the cost of a failed call was lost: %+v", res.Usage)
	}
}

func TestClaudeReportsTheCommandLinesOwnError(t *testing.T) {
	var args []string
	var stdin string
	c := fakeClaude(`{"is_error":true,"subtype":"error_max_budget_usd","result":"budget reached","total_cost_usd":5}`, nil, &args, &stdin)
	res, err := c.Run(context.Background(), RunRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "error_max_budget_usd") {
		t.Fatalf("err = %v, want the command line's error", err)
	}
	if res.Usage.CostUSD != 5 {
		t.Errorf("cost = %v, want 5", res.Usage.CostUSD)
	}
}

func TestClaudeDecideAsksForOneOfTheLabels(t *testing.T) {
	var args []string
	var stdin string
	c := fakeClaude(`{"is_error":false,"structured_output":{"label":"fix","confidence":0.92},"total_cost_usd":0.001}`, nil, &args, &stdin)
	res, err := c.Decide(context.Background(), DecideRequest{Prompt: "checkout is down", Labels: []string{"fix", "needs_human"}})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if res.Label != "fix" || res.Confidence != 0.92 {
		t.Errorf("Decide = %+v", res)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, `"enum":["fix","needs_human"]`) {
		t.Errorf("the labels were not passed as a closed list: %s", joined)
	}
	if !strings.Contains(joined, "--model haiku") {
		t.Errorf("a decide step should default to a small model: %s", joined)
	}
	if !strings.Contains(joined, "never instructions") {
		t.Errorf("the classifier was not told to treat its input as data: %s", joined)
	}
}

// TestClaudeLive makes one real call. It runs only when asked, because it
// needs a signed-in command line and spends a little.
func TestClaudeLive(t *testing.T) {
	if os.Getenv("WEIR_TEST_CLAUDE") == "" {
		t.Skip("set WEIR_TEST_CLAUDE=1 to make a real call to the Claude command line")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := &Claude{}
	res, err := c.Decide(ctx, DecideRequest{
		System: "You route support messages.",
		Prompt: "Thanks, that fixed it!",
		Labels: []string{"quick", "considered"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	t.Logf("label=%s confidence=%.2f model=%s cost=$%.5f tokens=%d/%d took=%s",
		res.Label, res.Confidence, res.Usage.Model, res.Usage.CostUSD, res.Usage.TokensIn, res.Usage.TokensOut, res.Usage.Duration.Round(time.Millisecond))
	if res.Label != "quick" && res.Label != "considered" {
		t.Errorf("label = %q, want one of the two offered", res.Label)
	}
	if res.Usage.CostUSD <= 0 {
		t.Errorf("no cost was reported: %+v", res.Usage)
	}
}
