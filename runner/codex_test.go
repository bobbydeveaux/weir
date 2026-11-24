package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

const codexEvents = `{"type":"thread.started","thread_id":"t1"}
{"type":"turn.started"}
{"type":"item.completed","item":{"type":"agent_message","text":"{}"}}
{"type":"turn.completed","usage":{"input_tokens":1200,"cached_input_tokens":1000,"output_tokens":40}}
`

// fakeCodex stands in for the command line: it writes answer to the file the
// adapter asked for and prints events.
func fakeCodex(answer, events string, seen *[]string, stdin *string) *Codex {
	return &Codex{run: func(_ context.Context, _ string, args, _ []string, in, _ string) ([]byte, error) {
		*seen, *stdin = args, in
		if i := slices.Index(args, "--output-last-message"); i >= 0 && answer != "" {
			_ = os.WriteFile(args[i+1], []byte(answer), 0o600)
		}
		return []byte(events), nil
	}}
}

func TestCodexRunReturnsTheAnswerAndUsage(t *testing.T) {
	var args []string
	var stdin string
	c := fakeCodex(`{"verdict": "approve"}`, codexEvents, &args, &stdin)
	c.USDPerMillionIn, c.USDPerMillionOut = 2, 10
	res, err := c.Run(context.Background(), RunRequest{System: "review carefully", Prompt: "review this diff",
		Schema: json.RawMessage(`{"type":"object"}`)})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Output) != `{"verdict": "approve"}` {
		t.Errorf("Output = %s", res.Output)
	}
	if res.Usage.TokensIn != 1200 || res.Usage.TokensOut != 40 {
		t.Errorf("tokens = %d/%d", res.Usage.TokensIn, res.Usage.TokensOut)
	}
	// 1,200 in at $2 a million plus 40 out at $10 a million.
	if want := 0.0028; res.Usage.CostUSD < want-1e-9 || res.Usage.CostUSD > want+1e-9 {
		t.Errorf("cost = %v, want %v", res.Usage.CostUSD, want)
	}
	if !res.Usage.CostEstimated || res.Usage.Provider != "codex" {
		t.Errorf("usage = %+v, want an estimated cost from codex", res.Usage)
	}
	if !strings.Contains(stdin, "review carefully") || !strings.Contains(stdin, "review this diff") {
		t.Errorf("stdin = %q", stdin)
	}
}

func TestCodexIsLockedDown(t *testing.T) {
	args := codexArgs("gpt-test", "/work", "/s.json", "/out.txt")
	joined := strings.Join(args, " ")
	for _, want := range []string{"--sandbox read-only", "--ephemeral", "--ignore-user-config", "--ignore-rules",
		"--disable shell_tool", "--disable browser_use", "--disable computer_use", "--disable plugins", "--model gpt-test"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args are missing %q: %s", want, joined)
		}
	}
	for _, banned := range []string{"dangerously", "full-auto", "danger-full-access", "workspace-write"} {
		if strings.Contains(joined, banned) {
			t.Errorf("args contain %q: %s", banned, joined)
		}
	}
}

func TestCodexRefusesAStepWithTools(t *testing.T) {
	var args []string
	var stdin string
	c := fakeCodex(`{}`, codexEvents, &args, &stdin)
	_, err := c.Run(context.Background(), RunRequest{Prompt: "x", Tools: []Tool{{Name: "scm.read_file"}}})
	if err == nil || !strings.Contains(err.Error(), "cannot offer tools") {
		t.Fatalf("err = %v, want the step refused", err)
	}
	if args != nil {
		t.Error("the command line was started although the step should have been refused")
	}
}

func TestCodexReportsItsOwnError(t *testing.T) {
	var args []string
	var stdin string
	c := fakeCodex("", `{"type":"error","message":"model not available"}`+"\n", &args, &stdin)
	_, err := c.Run(context.Background(), RunRequest{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "model not available") {
		t.Fatalf("err = %v, want Codex's own message", err)
	}
}

func TestByTierSendsEachStepToItsRunner(t *testing.T) {
	a := &Fake{Outputs: map[string][]json.RawMessage{"write": {json.RawMessage(`{"by": "a"}`)}}}
	b := &Fake{Outputs: map[string][]json.RawMessage{"review": {json.RawMessage(`{"by": "b"}`)}}}
	r := ByTier{Default: a, Tiers: map[string]Runner{"review": b}}

	if _, err := r.Run(context.Background(), RunRequest{Step: "write"}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Run(context.Background(), RunRequest{Step: "review", Tier: "review"}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(a.Calls, ",") != "write" || strings.Join(b.Calls, ",") != "review" {
		t.Fatalf("calls went to a=%v b=%v", a.Calls, b.Calls)
	}
}

// TestCodexLive makes one real call. It runs only when asked.
func TestCodexLive(t *testing.T) {
	if os.Getenv("WEIR_TEST_CODEX") == "" {
		t.Skip("set WEIR_TEST_CODEX=1 to make a real call to the Codex command line; WEIR_TEST_CODEX_MODEL picks the model")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c := &Codex{Model: os.Getenv("WEIR_TEST_CODEX_MODEL")}
	res, err := c.Decide(ctx, DecideRequest{
		System: "You route support messages.",
		Prompt: "Thanks, that fixed it!",
		Labels: []string{"quick", "considered"},
	})
	if err != nil {
		t.Fatalf("Decide: %v", err)
	}
	t.Logf("label=%s confidence=%.2f tokens=%d/%d took=%s", res.Label, res.Confidence,
		res.Usage.TokensIn, res.Usage.TokensOut, res.Usage.Duration.Round(time.Millisecond))
	if res.Label != "quick" && res.Label != "considered" {
		t.Errorf("label = %q, want one of the two offered", res.Label)
	}
	if res.Usage.TokensIn == 0 {
		t.Errorf("no token usage was read from the event stream: %+v", res.Usage)
	}
}

func TestCodexRefusesABudgetItCannotEnforce(t *testing.T) {
	budgeted := RunRequest{Prompt: "review this diff", BudgetUSD: 1}
	for name, prices := range map[string][2]float64{"no prices": {0, 0}, "only a price in": {2, 0}, "only a price out": {0, 10}} {
		var args []string
		var stdin string
		c := fakeCodex(`{}`, codexEvents, &args, &stdin)
		c.USDPerMillionIn, c.USDPerMillionOut = prices[0], prices[1]
		res, err := c.Run(context.Background(), budgeted)
		if !errors.Is(err, ErrNoBudget) {
			t.Fatalf("%s: err = %v, want the step refused with ErrNoBudget", name, err)
		}
		for _, setting := range []string{"WEIR_CODEX_USD_PER_MTOK_IN", "WEIR_CODEX_USD_PER_MTOK_OUT"} {
			if !strings.Contains(err.Error(), setting) {
				t.Errorf("%s: the refusal does not name %s: %v", name, setting, err)
			}
		}
		if args != nil {
			t.Errorf("%s: the command line was started although the step should have been refused", name)
		}
		if res.Usage.CostUSD != 0 || res.Usage.TokensIn != 0 {
			t.Errorf("%s: usage = %+v, want nothing spent", name, res.Usage)
		}
	}

	// With both prices the same step runs, and its cost is tokens times prices.
	var args []string
	var stdin string
	c := fakeCodex(`{"verdict": "approve"}`, codexEvents, &args, &stdin)
	c.USDPerMillionIn, c.USDPerMillionOut = 2, 10
	res, err := c.Run(context.Background(), budgeted)
	if err != nil {
		t.Fatalf("with prices: %v", err)
	}
	if args == nil {
		t.Fatal("with prices: the command line was not started")
	}
	// 1,200 in at $2 a million plus 40 out at $10 a million.
	if want := 0.0028; res.Usage.CostUSD < want-1e-9 || res.Usage.CostUSD > want+1e-9 || !res.Usage.CostEstimated {
		t.Errorf("with prices: usage = %+v, want an estimated cost of %v", res.Usage, want)
	}
}

// A call that carries no budget asks for no guarantee, so it is not refused.
func TestCodexWithNoPricesStillRunsACallThatHasNoBudget(t *testing.T) {
	var args []string
	var stdin string
	c := fakeCodex(`{"verdict": "approve"}`, codexEvents, &args, &stdin)
	res, err := c.Run(context.Background(), RunRequest{Prompt: "review this diff"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if args == nil {
		t.Fatal("the command line was not started")
	}
	if res.Usage.CostUSD != 0 || !res.Usage.CostEstimated || res.Usage.TokensIn != 1200 {
		t.Errorf("usage = %+v, want the tokens, and a cost of zero marked as estimated", res.Usage)
	}

	c = fakeCodex(`{"label": "quick", "confidence": 0.9}`, codexEvents, &args, &stdin)
	d, err := c.Decide(context.Background(), DecideRequest{Prompt: "thanks", Labels: []string{"quick", "considered"}})
	if err != nil || d.Label != "quick" || d.Usage.CostUSD != 0 {
		t.Errorf("Decide = %+v, %v; want the label and a cost of zero", d, err)
	}
}
