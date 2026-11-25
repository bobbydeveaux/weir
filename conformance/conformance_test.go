// Package conformance_test runs every case under cases/ against the engine.
// The cases are data, described in README.md, so that another implementation
// of the contract can run the same ones.
package conformance_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/internal/testdb"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/worker"
)

// conformanceCase is case.yaml: what goes in, what the world outside Weir
// does, and what must come out.
type conformanceCase struct {
	Description string   `yaml:"description"`
	Templates   []string `yaml:"templates"`
	// Connections are the third parties, each with the replies it gives in
	// turn. The last reply is repeated.
	Connections map[string][]any `yaml:"connections"`
	Model       struct {
		CostUSD   float64 `yaml:"cost_usd"`
		Decisions map[string]struct {
			Label      string  `yaml:"label"`
			Confidence float64 `yaml:"confidence"`
		} `yaml:"decisions"`
		Outputs map[string][]any `yaml:"outputs"`
	} `yaml:"model"`
	Steps  []action `yaml:"steps"`
	Expect struct {
		Status string `yaml:"status"`
		Error  string `yaml:"error"`
	} `yaml:"expect"`
}

// action is one thing done to Weir from outside. Exactly one field is set.
type action struct {
	Start *struct {
		Requester string `yaml:"requester"`
		Input     any    `yaml:"input"`
	} `yaml:"start"`
	Emit *struct {
		Type string `yaml:"type"`
		ID   string `yaml:"id"`
		Data any    `yaml:"data"`
	} `yaml:"emit"`
	Decide *struct {
		As      string `yaml:"as"`
		Outcome string `yaml:"outcome"`
		Refused bool   `yaml:"refused"`
	} `yaml:"decide"`
	Callback *struct {
		Connection string `yaml:"connection"`
		Output     any    `yaml:"output"`
	} `yaml:"callback"`
	Cancel *struct {
		As     string `yaml:"as"`
		Reason string `yaml:"reason"`
	} `yaml:"cancel"`
}

// party is a stand-in third party. It keeps the callback tokens it is sent.
type party struct {
	mu      sync.Mutex
	replies []any
	calls   int
	tokens  []string
}

func (p *party) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, r.Body)
	p.mu.Lock()
	var reply any = map[string]any{"accepted": true}
	if len(p.replies) > 0 {
		reply = p.replies[min(p.calls, len(p.replies)-1)]
	}
	p.calls++
	if token := r.Header.Get("Weir-Callback-Token"); token != "" {
		p.tokens = append(p.tokens, token)
	}
	p.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(reply)
}

func (p *party) token() (string, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.tokens) == 0 {
		return "", false
	}
	token := p.tokens[0]
	p.tokens = p.tokens[1:]
	return token, true
}

func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	if v == nil {
		return json.RawMessage(`{}`)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("a value in case.yaml cannot be written as JSON: %v", err)
	}
	return out
}

func TestConformance(t *testing.T) {
	dirs, err := filepath.Glob("cases/*/case.yaml")
	if err != nil || len(dirs) == 0 {
		t.Fatalf("no cases found under cases/ (%v)", err)
	}
	for _, file := range dirs {
		dir := filepath.Dir(file)
		t.Run(filepath.Base(dir), func(t *testing.T) { runCase(t, dir) })
	}
}

func runCase(t *testing.T, dir string) {
	source, err := os.ReadFile(filepath.Join(dir, "case.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	var c conformanceCase
	dec := yaml.NewDecoder(strings.NewReader(string(source)))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		t.Fatalf("case.yaml: %v", err)
	}
	definition, err := os.ReadFile(filepath.Join(dir, "workflow.yaml"))
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	eng, err := engine.New(ctx, testdb.New(ctx, t))
	if err != nil {
		t.Fatalf("engine.New: %v", err)
	}
	eng.BackoffBase = time.Millisecond
	reg := worker.NewRegistry()
	eng.HandlerExists = reg.Exists
	fake := &runner.Fake{Decisions: map[string]runner.DecideResult{}, Outputs: map[string][]json.RawMessage{}, CostUSD: c.Model.CostUSD}
	for step, d := range c.Model.Decisions {
		fake.Decisions[step] = runner.DecideResult{Label: d.Label, Confidence: d.Confidence}
	}
	for step, outputs := range c.Model.Outputs {
		for _, o := range outputs {
			fake.Outputs[step] = append(fake.Outputs[step], raw(t, o))
		}
	}
	w := &worker.Worker{Engine: eng, Runner: fake, Handlers: reg, ID: "conformance", Lease: 5 * time.Second,
		CallbackURL: "http://weir.test", PollEvery: 5 * time.Millisecond}

	parties := map[string]*party{}
	for name, replies := range c.Connections {
		p := &party{replies: replies}
		srv := httptest.NewServer(p)
		t.Cleanup(srv.Close)
		parties[name] = p
		if err := eng.PutConnection(ctx, name, srv.URL, engine.Auth{}); err != nil {
			t.Fatal(err)
		}
	}
	for _, key := range c.Templates {
		if _, err := eng.PublishTemplate(ctx, key, engine.Template{User: "{{input}}"}, "conformance"); err != nil {
			t.Fatalf("template %s: %v", key, err)
		}
	}
	published, err := eng.Publish(ctx, definition, "conformance")
	if err != nil {
		t.Fatalf("publish: %v", err)
	}

	settle := func() {
		t.Helper()
		idle := 0
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline) && idle < 8; {
			found, err := w.RunOnce(ctx)
			if err != nil {
				t.Fatalf("worker: %v", err)
			}
			if found {
				idle = 0
				continue
			}
			idle++
			time.Sleep(5 * time.Millisecond)
		}
	}

	var runID string
	for i, a := range c.Steps {
		at := fmt.Sprintf("steps[%d]", i)
		switch {
		case a.Start != nil:
			runID, _, err = eng.StartRun(ctx, engine.StartRequest{Namespace: published.Namespace, Name: published.Name,
				Requester: a.Start.Requester, Input: raw(t, a.Start.Input)})
			if err != nil {
				t.Fatalf("%s start: %v", at, err)
			}
		case a.Emit != nil:
			out, err := eng.Emit(ctx, engine.Event{Type: a.Emit.Type, ID: a.Emit.ID, Source: "conformance", Data: raw(t, a.Emit.Data)}, "conformance")
			if err != nil {
				t.Fatalf("%s emit: %v", at, err)
			}
			if runID == "" && len(out.Runs) == 1 {
				runID = out.Runs[0].RunID
			}
		case a.Decide != nil:
			tasks, err := eng.Tasks(ctx)
			if err != nil || len(tasks) != 1 {
				t.Fatalf("%s decide: %d tasks are open (%v), want 1", at, len(tasks), err)
			}
			err = eng.Decide(ctx, tasks[0].ID, a.Decide.As, a.Decide.Outcome, "")
			if a.Decide.Refused {
				if !errors.Is(err, engine.ErrNotAllowed) {
					t.Fatalf("%s decide as %s = %v, want it refused", at, a.Decide.As, err)
				}
			} else if err != nil {
				t.Fatalf("%s decide as %s: %v", at, a.Decide.As, err)
			}
		case a.Callback != nil:
			p := parties[a.Callback.Connection]
			if p == nil {
				t.Fatalf("%s callback: no connection %q in this case", at, a.Callback.Connection)
			}
			token, ok := p.token()
			if !ok {
				t.Fatalf("%s callback: %s has not been sent a token", at, a.Callback.Connection)
			}
			res := engine.Result{Output: raw(t, a.Callback.Output), Actor: "connection:" + a.Callback.Connection}
			if err := eng.Complete(ctx, token, res); err != nil {
				t.Fatalf("%s callback: %v", at, err)
			}
		case a.Cancel != nil:
			if err := eng.Cancel(ctx, runID, a.Cancel.As, a.Cancel.Reason); err != nil {
				t.Fatalf("%s cancel: %v", at, err)
			}
		default:
			t.Fatalf("%s names no action", at)
		}
		settle()
	}
	if runID == "" {
		t.Fatal("the case started no run")
	}

	v, err := eng.Run(ctx, runID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != c.Expect.Status {
		t.Errorf("status = %s, want %s", v.Status, c.Expect.Status)
	}
	var failure struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(v.Error, &failure)
	if failure.Type != c.Expect.Error {
		t.Errorf("error type = %q, want %q", failure.Type, c.Expect.Error)
	}
	var got strings.Builder
	for _, e := range v.Events {
		got.WriteString(strings.TrimSpace(e.Type+" "+e.Step) + "\n")
	}
	// WEIR_CONFORMANCE_RECORD=1 writes the log a case produced in place of
	// checking it. Read the difference before committing it: the file is the
	// contract, and recording a mistake makes the mistake the contract.
	logFile := filepath.Join(dir, "events.txt")
	if os.Getenv("WEIR_CONFORMANCE_RECORD") == "1" {
		if err := os.WriteFile(logFile, []byte(got.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("the case has no events.txt: %v", err)
	}
	if got.String() != string(want) {
		t.Errorf("event log:\n%s\nwant, from events.txt:\n%s", got.String(), want)
	}
}
