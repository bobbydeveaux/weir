package runner

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
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
	return &Claude{run: func(_ context.Context, _ string, args, _ []string, in, _ string) ([]byte, error) {
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
	args := strings.Join(claudeArgs("system", "opus", []byte(`{}`), 5, ""), " ")
	for _, banned := range []string{"dangerously", "bypassPermissions", "yolo"} {
		if strings.Contains(args, banned) {
			t.Errorf("args contain %q: %s", banned, args)
		}
	}
	if !strings.Contains(args, "--permission-mode dontAsk") {
		t.Errorf("args do not keep permission checks on: %s", args)
	}
	// "--tools" followed by an empty value switches every built-in tool off.
	list := claudeArgs("", "", nil, 0, "")
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

func TestClaudeSaysWhenItStoppedAtItsBudget(t *testing.T) {
	stopped := `{"is_error":true,"subtype":"error_max_budget_usd","result":"budget reached","total_cost_usd":5}`
	for name, exit := range map[string]error{"exiting cleanly": nil, "exiting badly": errors.New("exit status 1")} {
		var args []string
		var stdin string
		res, err := fakeClaude(stopped, exit, &args, &stdin).Run(context.Background(), RunRequest{Prompt: "x", BudgetUSD: 5})
		if !errors.Is(err, ErrBudget) {
			t.Errorf("%s: err = %v, want ErrBudget", name, err)
		}
		if res.Usage.CostUSD != 5 {
			t.Errorf("%s: cost = %v, want 5", name, res.Usage.CostUSD)
		}
	}
	var args []string
	var stdin string
	other := `{"is_error":true,"subtype":"error_during_execution","result":"it broke","total_cost_usd":1}`
	if _, err := fakeClaude(other, nil, &args, &stdin).Run(context.Background(), RunRequest{Prompt: "x", BudgetUSD: 5}); err == nil || errors.Is(err, ErrBudget) {
		t.Errorf("err = %v, want an error that is not ErrBudget", err)
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

func TestAnAgentGivenFilesMayReadOnlyThose(t *testing.T) {
	var args []string
	var dir, found string
	c := &Claude{run: func(_ context.Context, _ string, a, _ []string, _, d string) ([]byte, error) {
		args, dir = a, d
		data, _ := os.ReadFile(filepath.Join(d, "treaty.pdf"))
		found = string(data)
		return []byte(`{"result":"ok","structured_output":{"ok":true},"total_cost_usd":0.01}`), nil
	}}
	_, err := c.Run(context.Background(), RunRequest{Prompt: "read it", Schema: json.RawMessage(`{"type":"object"}`),
		Files: []File{{Name: "treaty.pdf", Data: []byte("%PDF")}}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if found != "%PDF" {
		t.Errorf("the file was not in the working directory %q", dir)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--tools Read ") || !strings.Contains(joined, "--allowedTools Read(/"+dir+"/**)") {
		t.Errorf("args do not confine reading to the file directory: %s", joined)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the file directory %q was left behind", dir)
	}
	if _, err := c.Run(context.Background(), RunRequest{Prompt: "x", Files: []File{{Name: "../escape.txt"}}}); err == nil {
		t.Error("a file name with a directory was accepted")
	}
}

func TestAModelCanBeNamedAsACloudServesIt(t *testing.T) {
	c := &Claude{Models: map[string]string{"small": "claude-haiku-5-5", "alias": "haiku"}, ModelPrefix: "eu.anthropic."}
	for _, tc := range []struct{ tier, fallback, want string }{
		{"small", "", "eu.anthropic.claude-haiku-5-5"},                           // a tier's model
		{"", "claude-sonnet-5-5", "eu.anthropic.claude-sonnet-5-5"},              // a template's model
		{"alias", "", "haiku"},                                                   // a short alias is left alone
		{"", "eu.anthropic.claude-sonnet-5-5", "eu.anthropic.claude-sonnet-5-5"}, // already prefixed
		{"", "", ""}, // none named: the command line's default
	} {
		if got := c.model(tc.tier, tc.fallback); got != tc.want {
			t.Errorf("model(%q, %q) = %q, want %q", tc.tier, tc.fallback, got, tc.want)
		}
	}
	if got := (&Claude{}).model("", "claude-sonnet-5-5"); got != "claude-sonnet-5-5" {
		t.Errorf("with no prefix the model became %q", got)
	}
}

func TestOnlyNamedVariablesReachAnAgent(t *testing.T) {
	t.Setenv("CLAUDE_CODE_USE_BEDROCK", "1")
	t.Setenv("WEIR_SECRET_THING", "do-not-leak")
	var env []string
	c := &Claude{Env: []string{"CLAUDE_CODE_USE_BEDROCK"}, run: func(_ context.Context, _ string, _, e []string, _, _ string) ([]byte, error) {
		env = e
		return []byte(`{"result":"ok","structured_output":{}}`), nil
	}}
	if _, err := c.Run(context.Background(), RunRequest{Prompt: "x"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(env, "\n")
	if !strings.Contains(joined, "CLAUDE_CODE_USE_BEDROCK=1") {
		t.Errorf("the variable that was named did not reach the agent: %s", joined)
	}
	if strings.Contains(joined, "WEIR_SECRET_THING") {
		t.Error("a variable that was not named reached the agent")
	}
}

func TestAnAgentThatMayWriteIsConfinedToOneDirectory(t *testing.T) {
	var args []string
	var dir, system string
	wrote := func(d string) {
		_ = os.MkdirAll(filepath.Join(d, "out", "src"), 0o700)
		_ = os.WriteFile(filepath.Join(d, "out", "README.md"), []byte("hello"), 0o600)
		_ = os.WriteFile(filepath.Join(d, "out", "src", "main.go"), []byte("package main"), 0o600)
		// Written outside the directory it was given: never kept.
		_ = os.WriteFile(filepath.Join(d, "stray.txt"), []byte("no"), 0o600)
	}
	c := &Claude{run: func(_ context.Context, _ string, a, _ []string, _, d string) ([]byte, error) {
		args, dir = a, d
		for i, v := range a {
			if v == "--system-prompt" {
				system = a[i+1]
			}
		}
		wrote(d)
		return []byte(`{"result":"ok","structured_output":{"ok":true},"total_cost_usd":0.01}`), nil
	}}
	req := RunRequest{System: "Be brief.", Prompt: "write it", Schema: json.RawMessage(`{"type":"object"}`),
		Files: []File{{Name: "spec/brief.md", Data: []byte("brief")}}, Writes: &WriteLimit{MaxFiles: 5, MaxBytes: 1 << 20}}
	res, err := c.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got := map[string]string{}
	for _, f := range res.Files {
		got[f.Name] = string(f.Data)
	}
	if len(got) != 2 || got["README.md"] != "hello" || got["src/main.go"] != "package main" {
		t.Errorf("files = %v, want only the two written inside out", got)
	}
	joined := strings.Join(args, " ")
	out := filepath.Join(dir, "out")
	for _, want := range []string{"--tools Read,Write,Edit ", "Read(/" + dir + "/**)", "Edit(/" + out + "/**)"} {
		if !strings.Contains(joined, want) {
			t.Errorf("args lack %q: %s", want, joined)
		}
	}
	if strings.Contains(joined, "Edit(/"+dir+"/**)") || strings.Contains(joined, "Write(") {
		t.Errorf("the agent may write to its whole working directory: %s", joined)
	}
	if !strings.Contains(system, "Be brief.") || !strings.Contains(system, out) {
		t.Errorf("the agent was not told where to write: %q", system)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("the working directory %q was left behind", dir)
	}

	// Too many files, too many bytes, and anything that is not a file.
	req.Writes = &WriteLimit{MaxFiles: 1, MaxBytes: 1 << 20}
	if _, err := c.Run(context.Background(), req); err == nil || !strings.Contains(err.Error(), "more than 1 files") {
		t.Errorf("two files with a limit of one = %v", err)
	}
	req.Writes = &WriteLimit{MaxFiles: 5, MaxBytes: 10}
	if _, err := c.Run(context.Background(), req); err == nil || !strings.Contains(err.Error(), "MB of files") {
		t.Errorf("17 bytes with a limit of 10 = %v", err)
	}
	req.Writes = &WriteLimit{MaxFiles: 5, MaxBytes: 1 << 20}
	wrote = func(d string) { _ = os.Symlink("/etc/hosts", filepath.Join(d, "out", "hosts")) }
	if _, err := c.Run(context.Background(), req); err == nil || !strings.Contains(err.Error(), "not an ordinary file") {
		t.Errorf("a link out of the directory = %v, want it refused", err)
	}
	// A file given to the agent cannot land in the directory it writes to.
	req.Files = []File{{Name: "out/planted.txt"}}
	if _, err := c.Run(context.Background(), req); err == nil {
		t.Error("a file given under out/ was accepted")
	}
}

// TestAnAgentWritesFilesLive has a real agent write a file. It runs only when
// asked, because it needs a signed-in command line and spends a little.
func TestAnAgentWritesFilesLive(t *testing.T) {
	if os.Getenv("WEIR_TEST_CLAUDE") == "" {
		t.Skip("set WEIR_TEST_CLAUDE=1 to make a real call to the Claude command line")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	c := &Claude{Models: map[string]string{"": "haiku"}}
	res, err := c.Run(ctx, RunRequest{
		System: "You write small files when asked.",
		Prompt: "Read brief.txt. Write a file called notes/answer.txt holding exactly the word in the brief, in capitals.",
		Schema: json.RawMessage(`{"type":"object","required":["wrote"],"properties":{"wrote":{"type":"string"}}}`),
		Files:  []File{{Name: "brief.txt", Data: []byte("heron")}},
		Writes: &WriteLimit{MaxFiles: 5, MaxBytes: 1 << 20},
	})
	t.Logf("output=%s cost=$%.4f files=%d transcript=%s", res.Output, res.Usage.CostUSD, len(res.Files), res.Transcript)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	for _, f := range res.Files {
		t.Logf("  %s: %q", f.Name, f.Data)
	}
	if len(res.Files) != 1 || res.Files[0].Name != "notes/answer.txt" || strings.TrimSpace(string(res.Files[0].Data)) != "HERON" {
		t.Errorf("files = %+v, want notes/answer.txt holding HERON", res.Files)
	}
}

func TestAShellIsOnlyEverGivenInsideASandbox(t *testing.T) {
	var args []string
	reply := func(_ context.Context, _ string, a, _ []string, _, _ string) ([]byte, error) {
		args = a
		return []byte(`{"result":"ok","structured_output":{"ok":true},"total_cost_usd":0.01}`), nil
	}
	req := RunRequest{Prompt: "run the tests", Schema: json.RawMessage(`{"type":"object"}`), Shell: &Shell{Hosts: []string{"proxy.golang.org"}}}

	// Off unless whoever runs the worker turned it on.
	off := &Claude{run: reply}
	if _, err := off.Run(context.Background(), req); !errors.Is(err, ErrNoShell) || args != nil {
		t.Fatalf("a shell on a server that does not allow one = %v, ran %v", err, args)
	}
	// On Linux without the sandbox's programs: refused, never run unsandboxed.
	bare := &Claude{Shell: true, os: "linux", run: reply, look: func(string) (string, error) { return "", errors.New("not found") }}
	if _, err := bare.Run(context.Background(), req); !errors.Is(err, ErrNoShell) || args != nil {
		t.Fatalf("a shell with no sandbox to run it in = %v, ran %v", err, args)
	}
	if _, err := (&Claude{Shell: true, os: "windows", run: reply}).Run(context.Background(), req); !errors.Is(err, ErrNoShell) {
		t.Fatalf("a shell on a system with no sandbox = %v", err)
	}

	c := &Claude{Shell: true, os: "linux", Hidden: []string{"/var/weir/secrets"}, run: reply, look: func(string) (string, error) { return "/usr/bin/x", nil }}
	if _, err := c.Run(context.Background(), req); err != nil {
		t.Fatalf("Run: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--tools Read,Bash ") {
		t.Errorf("the shell was not offered: %s", joined)
	}
	var raw string
	for i, a := range args {
		if a == "--settings" {
			raw = args[i+1]
		}
	}
	var settings struct {
		Sandbox struct {
			Enabled                  bool
			AutoAllowBashIfSandboxed bool
			AllowUnsandboxedCommands bool
			Network                  struct{ AllowedDomains []string }
			Filesystem               struct{ DenyRead []string }
		}
	}
	if err := json.Unmarshal([]byte(raw), &settings); err != nil {
		t.Fatalf("settings %q: %v", raw, err)
	}
	s := settings.Sandbox
	if !s.Enabled || s.AllowUnsandboxedCommands || s.AutoAllowBashIfSandboxed {
		t.Errorf("the sandbox can be left: %s", raw)
	}
	if len(s.Network.AllowedDomains) != 1 || s.Network.AllowedDomains[0] != "proxy.golang.org" {
		t.Errorf("hosts = %v", s.Network.AllowedDomains)
	}
	hidden := strings.Join(s.Filesystem.DenyRead, " ")
	if !strings.Contains(hidden, "/var/weir/secrets") || !strings.Contains(hidden, "~/.ssh") || !strings.Contains(hidden, "~/.aws") {
		t.Errorf("hidden from the shell = %v", s.Filesystem.DenyRead)
	}
	if strings.Contains(joined, "dangerously") || strings.Contains(joined, "bypassPermissions") {
		t.Errorf("permission checks were switched off: %s", joined)
	}

	// A shell with no hosts has no network at all: an empty list, not a missing one.
	req.Shell = &Shell{}
	if _, err := c.Run(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if joined = strings.Join(args, " "); !strings.Contains(joined, `"allowedDomains":[]`) {
		t.Errorf("a shell with no hosts was not given an empty list: %s", joined)
	}
}

// TestAnAgentsShellIsSandboxedLive has a real agent probe its own sandbox. It
// runs only when asked: it needs a signed-in command line and spends a little.
func TestAnAgentsShellIsSandboxedLive(t *testing.T) {
	if os.Getenv("WEIR_TEST_CLAUDE") == "" {
		t.Skip("set WEIR_TEST_CLAUDE=1 to make a real call to the Claude command line")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	secrets := t.TempDir()
	if real, err := filepath.EvalSymlinks(secrets); err == nil {
		secrets = real
	}
	if err := os.WriteFile(filepath.Join(secrets, "token"), []byte("heron-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	escape := filepath.Join(home, "weir-shell-escape.txt")
	t.Cleanup(func() { os.Remove(escape) })
	t.Setenv("WEIR_DATABASE_URL", "postgres://should-never-be-seen")

	c := &Claude{Models: map[string]string{"": "haiku"}, Shell: true, Hidden: []string{secrets}}
	probe := func(name string) map[string]any {
		return map[string]any{"type": "object", "required": []string{"exit", "output"}, "description": name,
			"properties": map[string]any{"exit": map[string]any{"type": "integer"}, "output": map[string]any{"type": "string"}}}
	}
	names := []string{"inside", "home", "allowed_host", "other_host", "metadata", "secret", "environment"}
	props := map[string]any{}
	for _, n := range names {
		props[n] = probe(n)
	}
	schema, _ := json.Marshal(map[string]any{"type": "object", "required": names, "properties": props})
	res, err := c.Run(ctx, RunRequest{
		System: "You are testing the sandbox your shell runs in. Run every command exactly as given, once each, and report its exit status and the first line of what it printed (standard output or error). Never skip one and never retry.",
		Prompt: "inside: echo hello > inside.txt && cat inside.txt\n" +
			"home: echo x > " + escape + "\n" +
			"allowed_host: curl -sS -m 15 -o /dev/null -w '%{http_code}' https://proxy.golang.org/\n" +
			"other_host: curl -sS -m 15 -o /dev/null -w '%{http_code}' https://example.com/\n" +
			"metadata: curl -sS -m 5 -o /dev/null -w '%{http_code}' http://169.254.169.254/\n" +
			"secret: cat " + filepath.Join(secrets, "token") + "\n" +
			"environment: env | grep -c WEIR_ ; true",
		Schema: schema,
		Shell:  &Shell{Hosts: []string{"proxy.golang.org"}},
	})
	t.Logf("cost=$%.4f output=%s", res.Usage.CostUSD, res.Output)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var got map[string]struct {
		Exit   int
		Output string
	}
	if err := json.Unmarshal(res.Output, &got); err != nil {
		t.Fatal(err)
	}
	if got["inside"].Exit != 0 || !strings.Contains(got["inside"].Output, "hello") {
		t.Errorf("writing inside the working directory = %+v", got["inside"])
	}
	if got["allowed_host"].Exit != 0 || !strings.Contains(got["allowed_host"].Output, "200") {
		t.Errorf("reaching a listed host = %+v", got["allowed_host"])
	}
	for _, refused := range []string{"home", "other_host", "metadata", "secret"} {
		if got[refused].Exit == 0 {
			t.Errorf("%s was not refused: %+v", refused, got[refused])
		}
	}
	if strings.Contains(string(res.Output), "heron-secret") {
		t.Error("the shell read a secret")
	}
	if _, err := os.Stat(escape); err == nil {
		t.Error("the shell wrote outside its working directory")
	}
	if !strings.HasPrefix(strings.TrimSpace(got["environment"].Output), "0") {
		t.Errorf("the shell saw the worker's environment: %+v", got["environment"])
	}
}

func TestAnAgentInItsOwnContainerIsGivenEveryTool(t *testing.T) {
	var args []string
	var dir string
	c := &Claude{Unrestricted: true, run: func(_ context.Context, _ string, a, _ []string, _, d string) ([]byte, error) {
		args, dir = a, d
		_ = os.WriteFile(filepath.Join(d, "main.go"), []byte("package main // fixed\n"), 0o600)
		return []byte(`{"result":"ok","structured_output":{"ok":true},"total_cost_usd":0.01}`), nil
	}}
	res, err := c.Run(context.Background(), RunRequest{System: "Be brief.", Prompt: "fix it", Schema: json.RawMessage(`{"type":"object"}`),
		Workspace: &Workspace{Prepare: repo}, Writes: &WriteLimit{MaxFiles: 10, MaxBytes: 1 << 20}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--dangerously-skip-permissions") || strings.Contains(joined, "--tools") ||
		strings.Contains(joined, "--permission-mode") || strings.Contains(joined, "sandbox") {
		t.Errorf("an unrestricted agent was restricted: %s", joined)
	}
	// It keeps the command line's own guidance, and is told where it is working.
	if !strings.Contains(joined, "--append-system-prompt Be brief.") || !strings.Contains(joined, dir) {
		t.Errorf("args = %s", joined)
	}
	if len(res.Files) != 1 || res.Files[0].Name != "main.go" {
		t.Errorf("changes = %+v", res.Files)
	}
	// Still no user or project settings, and no MCP servers but Weir's.
	if !strings.Contains(joined, "--strict-mcp-config") || !strings.Contains(joined, "--setting-sources") {
		t.Errorf("args = %s", joined)
	}
}

func TestAnAgentInItsOwnContainerCanKeepPermissionChecksOn(t *testing.T) {
	var args []string
	c := &Claude{Unrestricted: true, Permissions: PermissionsAllow, Bridge: []string{"weir", "mcp"},
		run: func(_ context.Context, _ string, a, _ []string, _, d string) ([]byte, error) {
			args = a
			_ = os.WriteFile(filepath.Join(d, "main.go"), []byte("package main // fixed\n"), 0o600)
			return []byte(`{"result":"ok","structured_output":{"ok":true},"total_cost_usd":0.01}`), nil
		}}
	res, err := c.Run(context.Background(), RunRequest{System: "Be brief.", Prompt: "fix it", Effort: "high", Schema: json.RawMessage(`{"type":"object"}`),
		Tools:     []Tool{{Name: "tickets.search"}},
		Workspace: &Workspace{Prepare: repo}, Writes: &WriteLimit{MaxFiles: 10, MaxBytes: 1 << 20}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	joined := strings.Join(args, " ")
	// Permission checks are not bypassed, and nothing waits for a person.
	if strings.Contains(joined, "dangerously") || strings.Contains(joined, "bypassPermissions") || !strings.Contains(joined, "--permission-mode dontAsk") {
		t.Errorf("permission checks: %s", joined)
	}
	// Every built-in tool is allowed by name, beside the step's own, in one list.
	if strings.Count(joined, "--allowedTools") != 1 ||
		!strings.Contains(joined, "--allowedTools mcp__weir__tickets_search Bash Read Edit Write NotebookEdit Glob Grep WebFetch WebSearch Skill --effort high") {
		t.Errorf("allowed tools: %s", joined)
	}
	// It is still not limited to a list of tools, and has no sandbox.
	if strings.Contains(joined, "--tools") || strings.Contains(joined, "sandbox") {
		t.Errorf("the agent was restricted: %s", joined)
	}
	// The rest is as for any agent in a container.
	if !strings.Contains(joined, "--append-system-prompt Be brief.") || !strings.Contains(joined, "--setting-sources user") || !strings.Contains(joined, "--strict-mcp-config") {
		t.Errorf("args = %s", joined)
	}
	if len(res.Files) != 1 || res.Files[0].Name != "main.go" {
		t.Errorf("changes = %+v", res.Files)
	}

	// With no tools of its own, the step's agent is still allowed the built-in ones.
	c.Bridge = nil
	if _, err := c.Run(context.Background(), RunRequest{Prompt: "fix it", Schema: json.RawMessage(`{"type":"object"}`), Workspace: &Workspace{Prepare: repo}}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if joined := strings.Join(args, " "); !strings.Contains(joined, "--allowedTools Bash Read ") {
		t.Errorf("allowed tools with none listed: %s", joined)
	}
}

func TestAnAgentInItsOwnContainerBypassesPermissionChecksUnlessTold(t *testing.T) {
	for _, permissions := range []string{"", PermissionsBypass} {
		var args []string
		c := &Claude{Unrestricted: true, Permissions: permissions, run: func(_ context.Context, _ string, a, _ []string, _, _ string) ([]byte, error) {
			args = a
			return []byte(`{"result":"ok","structured_output":{"ok":true}}`), nil
		}}
		if _, err := c.Run(context.Background(), RunRequest{Prompt: "fix it", Schema: json.RawMessage(`{"type":"object"}`)}); err != nil {
			t.Fatalf("Run with %q: %v", permissions, err)
		}
		joined := strings.Join(args, " ")
		if !strings.Contains(joined, "--dangerously-skip-permissions") || strings.Contains(joined, "--permission-mode") || strings.Contains(joined, "--allowedTools") {
			t.Errorf("with %q: %s", permissions, joined)
		}
	}
	called := false
	c := &Claude{Unrestricted: true, Permissions: "off", run: func(context.Context, string, []string, []string, string, string) ([]byte, error) {
		called = true
		return nil, nil
	}}
	_, err := c.Run(context.Background(), RunRequest{Prompt: "fix it"})
	if err == nil || !strings.Contains(err.Error(), "WEIR_AGENT_PERMISSIONS") || called {
		t.Errorf("an unknown value: err = %v, called = %v", err, called)
	}
}
