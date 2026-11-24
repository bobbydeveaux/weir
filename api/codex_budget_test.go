package api_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

const codexBudgetWorkflow = `
weir: "0.1"
namespace: code
name: review
steps:
  - id: review
    kind: ai
    tier: codex
    prompt: review@1
    retry: { attempts: 3 }
    output:
      schema: { type: object }
    budget: { usd: 1 }
`

// A stand-in for the Codex command line. It notes that it was started, writes
// an answer where it was asked to, and reports a million tokens in.
const scriptedCodex = `#!/bin/sh
echo started >> "$(dirname "$0")/started"
while [ $# -gt 0 ]; do
  if [ "$1" = "--output-last-message" ]; then out="$2"; fi
  shift
done
printf '%s' '{"ok": true}' > "$out"
echo '{"type":"turn.completed","usage":{"input_tokens":1000000,"output_tokens":0}}'
`

func TestACodexStepIsHeldToItsBudgetOrRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stand-in for the Codex command line is a shell script")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	eng, err := engine.New(ctx, testdb.New(ctx, t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.PublishTemplate(ctx, "review", engine.Template{System: "s", User: "u"}, "test"); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Publish(ctx, []byte(codexBudgetWorkflow), "test"); err != nil {
		t.Fatalf("publish: %v", err)
	}
	dir := t.TempDir()
	command := filepath.Join(dir, "codex")
	if err := os.WriteFile(command, []byte(scriptedCodex), 0o700); err != nil {
		t.Fatal(err)
	}
	started := func() int {
		raw, _ := os.ReadFile(filepath.Join(dir, "started"))
		return strings.Count(string(raw), "started")
	}
	// settle starts a run and gives the worker the step's three attempts.
	settle := func(codex *runner.Codex) string {
		id, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "code", Name: "review", Requester: "alice"})
		if err != nil {
			t.Fatal(err)
		}
		w := &worker.Worker{Engine: eng, Handlers: worker.NewRegistry(), Runner: codex}
		for i := 0; i < 3; i++ {
			if _, err := w.RunOnce(ctx); err != nil {
				t.Fatal(err)
			}
		}
		v, err := eng.Run(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return string(mustJSON(v))
	}

	// With no prices, Codex cannot tell what the step costs. The step is
	// refused before the command line is started, and not tried again.
	view := settle(&runner.Codex{Command: command})
	if started() != 0 {
		t.Errorf("the command line was started %d time(s) with no prices; run %s", started(), view)
	}
	if !strings.Contains(view, "budget_unenforceable") || !strings.Contains(view, "WEIR_CODEX_USD_PER_MTOK_IN") ||
		!strings.Contains(view, `"attempt":1`) || strings.Contains(view, `"attempt":2`) {
		t.Errorf("with no prices, the run is %s; want one attempt, failed as budget_unenforceable, naming the setting", view)
	}

	// With prices, a million tokens in at $2 a million is $2: more than the
	// step's $1. The call cannot be stopped part-way, so it runs once, and
	// the step then fails and is not tried again.
	view = settle(&runner.Codex{Command: command, USDPerMillionIn: 2, USDPerMillionOut: 10})
	if started() != 1 {
		t.Errorf("the command line was started %d time(s), want once; run %s", started(), view)
	}
	if !strings.Contains(view, "budget_exceeded") || !strings.Contains(view, `"spent_usd":2`) {
		t.Errorf("with prices, the run is %s; want it failed as budget_exceeded having spent $2", view)
	}
}
