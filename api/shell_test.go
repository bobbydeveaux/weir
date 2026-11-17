package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

const shellWorkflow = `
weir: "0.1"
namespace: code
name: run-tests
steps:
  - id: test
    kind: ai
    prompt: run-tests@1
    shell:
      hosts: [proxy.golang.org, "*.githubusercontent.com"]
    retry: { attempts: 3 }
    output:
      schema: { type: object }
    budget: { usd: 1 }
`

// shellRunner notes the shell it was asked for, and can refuse to give one.
type shellRunner struct {
	asked  *runner.Shell
	calls  int
	refuse bool
}

func (s *shellRunner) Name() string { return "fake" }
func (s *shellRunner) Decide(context.Context, runner.DecideRequest) (runner.DecideResult, error) {
	return runner.DecideResult{}, nil
}
func (s *shellRunner) Run(_ context.Context, req runner.RunRequest) (runner.RunResult, error) {
	s.asked, s.calls = req.Shell, s.calls+1
	if s.refuse {
		return runner.RunResult{}, fmt.Errorf("%w: this server does not allow agents a shell", runner.ErrNoShell)
	}
	return runner.RunResult{Output: json.RawMessage(`{"ok": true}`)}, nil
}

func TestAStepsShellReachesItsAgentAndARefusalIsNotRetried(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	eng, err := engine.New(ctx, testdb.New(ctx, t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.PublishTemplate(ctx, "run-tests", engine.Template{System: "s", User: "u"}, "test"); err != nil {
		t.Fatal(err)
	}
	for bad, want := range map[string]string{
		`hosts: ["https://proxy.golang.org"]`: "is not a host name",
		`hosts: ["*"]`:                        "is not a host name",
		`hosts: [localhost]`:                  "is not a host name",
	} {
		changed := strings.Replace(shellWorkflow, `hosts: [proxy.golang.org, "*.githubusercontent.com"]`, bad, 1)
		if _, err := eng.Publish(ctx, []byte(changed), "test"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("publish with %s = %v, want it refused", bad, err)
		}
	}
	if _, err := eng.Publish(ctx, []byte(shellWorkflow), "test"); err != nil {
		t.Fatalf("publish: %v", err)
	}

	start := func() string {
		id, _, err := eng.StartRun(ctx, engine.StartRequest{Namespace: "code", Name: "run-tests", Requester: "alice"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	agent := &shellRunner{}
	w := &worker.Worker{Engine: eng, Handlers: worker.NewRegistry(), Runner: agent}
	runID := start()
	if _, err := w.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if agent.asked == nil || strings.Join(agent.asked.Hosts, ",") != "proxy.golang.org,*.githubusercontent.com" {
		t.Errorf("the agent was asked for the shell %+v", agent.asked)
	}
	if v, _ := eng.Run(ctx, runID); v.Status != "succeeded" {
		t.Errorf("run = %s", v.Status)
	}

	// A worker that will not give a shell fails the step once. Trying again
	// cannot help, so the step's three attempts are not spent on it.
	agent.refuse, agent.calls = true, 0
	runID = start()
	for i := 0; i < 3; i++ {
		if _, err := w.RunOnce(ctx); err != nil {
			t.Fatal(err)
		}
	}
	v, err := eng.Run(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if agent.calls != 1 || v.Status == "succeeded" || !strings.Contains(string(mustJSON(v)), "does not allow agents a shell") {
		t.Errorf("after a refusal: %d call(s), run %s", agent.calls, mustJSON(v))
	}
}
