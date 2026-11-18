// Command weir is the engine's command line.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bobbydeveaux/weir/agent"
	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/blob"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/engine/store"
	"github.com/bobbydeveaux/weir/internal/config"
	"github.com/bobbydeveaux/weir/internal/mcpbridge"
	"github.com/bobbydeveaux/weir/runner"
	"github.com/bobbydeveaux/weir/spec"
	"github.com/bobbydeveaux/weir/worker"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `weir is a workflow engine built for AI.

Usage:
  weir <command> [arguments]

Setting up:
  migrate                          Bring the database up to the latest schema
  serve                            Run the API and a worker

Defining work:
  publish <file.yaml>              Publish a workflow as a new version
  template <key> --user <text>     Publish a prompt or decide template as a new version
  connection <name> <base-url>     Create or update a third party
                                   [--auth bearer|header|basic|github-app] [--secret env:NAME|file:name] [--app <id>]
                                   [--header <name>] [--username <name>] [--mcp] [--read-only] [--workspace default]
                                   [--webhook github|hmac|none] [--webhook-secret env:NAME|file:name]
                                   [--webhook-header <name>] [--webhook-event-header <name>]

Running work:
  run <namespace>/<name>           Start a run            [--input <json>] [--key <idempotency-key>]
  status <run-id>                  Show a run             [--events]
  tasks                            List steps waiting for a person
  approve <task-id>                Decide a waiting step  [--outcome approved] [--note <text>]
  resume <run-id>                  Restart a blocked run  [--budget <usd>]

Access:
  key create <name>                Make an API key        [--workspace default] [--role writer] [--act-as]
  key list                         List a workspace's keys [--workspace default]
  key revoke <key-id>              Stop a key working     [--workspace default]

Other:
  version                          Print the version

Most commands take --as <name> to say who is acting. It defaults to $USER.

Environment:
  WEIR_DATABASE_URL  Postgres connection string (required)
  WEIR_HTTP_ADDR     Address the API listens on (default :8080)
  WEIR_PUBLIC_URL    Address third parties use to reach the API (default http://localhost:8080)
  WEIR_RUNNER        How ai and decide steps run: cli, api or fake (default cli)
  WEIR_ADMIN_KEY     Turns authentication on, and is the key that may do anything
  WEIR_WORKER        off: answer the API and leave the work to another server on the same database
  WEIR_CONSOLE_DIR   Directory of the console's files, to serve the console from this server
  WEIR_PROXY_USER_HEADER
                     Header a sign-in proxy sets to say who is asking. That person needs no key.
                     Only safe where nothing but the proxy can reach this server
  WEIR_API_PREFIX    A path the API also answers under, such as /api, for a proxy that needs one
  WEIR_BLOB_URL      Where files are kept: s3://bucket, gs://bucket, azblob://container or
                     file:///directory (default: in Postgres)
  WEIR_SECRETS_DIR   Directory of secret files, one directory per workspace, for file: references
  WEIR_SECRET__<WORKSPACE>__<NAME>
                     The value of a connection's secret named env:<NAME> in that workspace
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// openBucket opens the bucket files are kept in. It is nil in a build made
// without the cloud storage libraries.
var openBucket func(ctx context.Context, address string) (blob.Store, error)

// app is one invocation of the command line.
type app struct {
	ctx    context.Context
	getenv func(string) string
	out    io.Writer
	in     io.Reader
	// reg holds the handlers and tools this process provides.
	reg *worker.Registry
}

// run executes one command and returns the process exit code.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	a := &app{ctx: ctx, getenv: getenv, out: stdout, in: os.Stdin}
	commands := map[string]func([]string) error{
		"migrate": a.migrate, "serve": a.serve, "publish": a.publish, "template": a.template,
		"connection": a.connection, "run": a.start, "status": a.status, "tasks": a.tasks,
		"approve": a.approve, "resume": a.resume, "mcp": a.mcp, "agent": a.agent, "key": a.key,
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprintf(stderr, "weir: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
	if err := cmd(args[1:]); err != nil {
		fmt.Fprintln(stderr, "weir:", err)
		return 1
	}
	return 0
}

// flags returns a flag set with the --as flag that most commands share.
func (a *app) flags(name string) (*flag.FlagSet, *string) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	who := fs.String("as", a.getenv("USER"), "who is acting")
	return fs, who
}

// parse reads flags that may come before or after the positional arguments.
func parse(fs *flag.FlagSet, args []string, positional int) ([]string, error) {
	var pos []string
	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) > 0 {
			pos = append(pos, args[0])
			args = args[1:]
		}
	}
	if len(pos) != positional {
		return nil, fmt.Errorf("%s takes %d argument(s), got %d; run `weir help`", fs.Name(), positional, len(pos))
	}
	return pos, nil
}

// engine connects to the database and returns an engine on it.
func (a *app) engine() (*engine.Engine, config.Config, func(), error) {
	cfg, err := config.FromEnv(a.getenv)
	if err != nil {
		return nil, cfg, nil, err
	}
	pool, err := pgxpool.New(a.ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, cfg, nil, fmt.Errorf("connect to the database: %w", err)
	}
	eng, err := engine.New(a.ctx, pool)
	if err != nil {
		pool.Close()
		return nil, cfg, nil, err
	}
	// Refuse workflows that name a handler or a tool no worker can provide.
	a.reg = worker.NewRegistry()
	worker.RegisterHTTPTools(a.reg, eng, nil)
	worker.RegisterGitHubTools(a.reg, eng, nil)
	worker.RegisterGitHubCheckout(a.reg, eng, nil)
	worker.RegisterGitHubCommit(a.reg, eng, nil)
	eng.HandlerExists, eng.ToolClass = a.reg.Exists, a.reg.ToolClass
	eng.WorkspaceExists = a.reg.WorkspaceExists
	return eng, cfg, pool.Close, nil
}

func (a *app) migrate([]string) error {
	cfg, err := config.FromEnv(a.getenv)
	if err != nil {
		return err
	}
	v, err := store.Migrate(a.ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "schema is at version %d\n", v)
	return nil
}

func (a *app) serve([]string) error {
	eng, cfg, done, err := a.engine()
	if err != nil {
		return err
	}
	defer done()

	var r runner.Runner
	decideWith := "scripted answers"
	// modelFor chooses a step's model, and agentEnv names what an agent's
	// container is given, when agents run in containers of their own.
	var modelFor func(tier, model string) string
	agentEnv := []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY"}
	switch cfg.Runner {
	case config.RunnerCLI:
		// Claude runs steps by default. A step with `tier: codex` goes to Codex,
		// so one workflow can have one model write a change and another review it.
		// An agent's command line sees almost none of this server's
		// environment. WEIR_AGENT_ENV names what else to let through: the
		// variables that send Claude to Bedrock or Vertex AI on the machine's
		// own cloud identity, or to an AI gateway.
		passed := names(a.getenv("WEIR_AGENT_ENV"))
		models := map[string]string{"small": "haiku", "large": "opus"}
		for tier, model := range pairs(a.getenv("WEIR_CLAUDE_MODELS")) {
			models[tier] = model
		}
		claude := &runner.Claude{Models: models, ModelPrefix: a.getenv("WEIR_CLAUDE_MODEL_PREFIX"), Env: passed}
		modelFor, agentEnv = claude.ModelFor, append(agentEnv, passed...)
		// A shell for agents is off unless asked for. When it is on, it can
		// never read the directory that holds workspaces' secrets.
		claude.Shell = a.getenv("WEIR_AGENT_SHELL") == "on"
		if dir := a.getenv("WEIR_SECRETS_DIR"); dir != "" {
			claude.Hidden = append(claude.Hidden, dir)
		}
		codex := &runner.Codex{Model: a.getenv("WEIR_CODEX_MODEL"), Env: passed}
		// A decide step goes to the provider its workflow names. Steps that
		// name none use Jev when it has a key, and Claude's small model otherwise.
		router := runner.Router{
			Default:  runner.ByTier{Default: claude, Tiers: map[string]runner.Runner{"codex": codex}},
			Deciders: map[string]runner.Runner{"claude": claude, "codex": codex},
		}
		for service, env := range map[string]string{runner.ServiceJev: "WEIR_TYPESAFE_KEY", runner.ServiceGeminiDecision: "WEIR_GEMINI_API_KEY"} {
			if key := a.getenv(env); key != "" {
				// The address can be changed, to go through a gateway or a proxy.
				router.Deciders[service] = &runner.Decisions{Service: service, Key: key, BaseURL: a.getenv(strings.TrimSuffix(env, "KEY") + "URL")}
			} else {
				router.Deciders[service] = runner.NotSetUp{Provider: service, Hint: "set " + env}
			}
		}
		router.DecideDefault = a.getenv("WEIR_DECIDE_PROVIDER")
		if router.DecideDefault == "" && a.getenv("WEIR_TYPESAFE_KEY") != "" {
			router.DecideDefault = runner.ServiceJev
		}
		if _, ok := router.Deciders[router.DecideDefault]; router.DecideDefault != "" && !ok {
			return fmt.Errorf("WEIR_DECIDE_PROVIDER is %q; it must be one of %s", router.DecideDefault, strings.Join(spec.DecideProviders, ", "))
		}
		decideWith = router.DecideDefault
		if decideWith == "" {
			decideWith = "claude"
		}
		r = router
	case config.RunnerFake:
		r = &runner.Fake{}
	default:
		return fmt.Errorf("the %s runner: %w", cfg.Runner, runner.ErrNotBuilt)
	}
	public := a.getenv("WEIR_PUBLIC_URL")
	if public == "" {
		public = "http://localhost" + cfg.HTTPAddr
	}
	// Agents on this machine reach their tools at the API's own address.
	w := &worker.Worker{Engine: eng, Runner: r, Handlers: a.reg, ID: "weir-serve", CallbackURL: public,
		ToolURL: "http://localhost" + cfg.HTTPAddr}

	// Where agents run. Beside this worker unless a launcher is named, in
	// which case each runs in a container of its own, with every tool and
	// nothing of Weir's but the token for its own step.
	agents := "beside this worker"
	switch where := a.getenv("WEIR_AGENT_RUNNER"); where {
	case "", "inline":
	case "docker":
		image := a.getenv("WEIR_AGENT_IMAGE")
		if image == "" {
			return errors.New("WEIR_AGENT_RUNNER is docker, so WEIR_AGENT_IMAGE must name the image agents run in")
		}
		launcher := &worker.Docker{Image: image, Env: agentEnv, Args: strings.Fields(a.getenv("WEIR_AGENT_DOCKER_ARGS"))}
		w.Launcher, w.Model = launcher, modelFor
		// A container reaches a Weir on this machine by this name.
		if w.AgentURL = a.getenv("WEIR_AGENT_URL"); w.AgentURL == "" {
			w.AgentURL = "http://host.docker.internal" + cfg.HTTPAddr
		}
		agents = "in " + launcher.Describe()
	case "kubernetes":
		image := a.getenv("WEIR_AGENT_IMAGE")
		if image == "" || a.getenv("WEIR_AGENT_URL") == "" {
			return errors.New("WEIR_AGENT_RUNNER is kubernetes, so WEIR_AGENT_IMAGE (the image agents run in) and WEIR_AGENT_URL (Weir's address inside the cluster) are both needed")
		}
		// The model's credential reaches an agent's pod from a Secret named
		// here. Weir itself never reads it.
		launcher := &worker.Kubernetes{Image: image, Namespace: a.getenv("WEIR_AGENT_NAMESPACE"), Secret: a.getenv("WEIR_AGENT_SECRET"),
			ServiceAccount: a.getenv("WEIR_AGENT_SERVICE_ACCOUNT"), CPU: a.getenv("WEIR_AGENT_CPU"), Memory: a.getenv("WEIR_AGENT_MEMORY")}
		w.Launcher, w.Model, w.AgentURL = launcher, modelFor, a.getenv("WEIR_AGENT_URL")
		agents = "in " + launcher.Describe()
	case "cloudrun":
		// The job is made once by whoever runs Weir, with its image, its own
		// service account and the model's credential. Weir only runs it.
		job := a.getenv("WEIR_AGENT_JOB")
		if job == "" || a.getenv("WEIR_AGENT_URL") == "" {
			return errors.New("WEIR_AGENT_RUNNER is cloudrun, so WEIR_AGENT_JOB (the Cloud Run job agents run as) and WEIR_AGENT_URL (Weir's own address) are both needed")
		}
		launcher := &worker.CloudRun{Job: job, Project: a.getenv("WEIR_AGENT_PROJECT"), Region: a.getenv("WEIR_AGENT_REGION")}
		w.Launcher, w.Model, w.AgentURL = launcher, modelFor, a.getenv("WEIR_AGENT_URL")
		agents = "in " + launcher.Describe()
	default:
		return fmt.Errorf("WEIR_AGENT_RUNNER is %q; it must be inline, docker, kubernetes or cloudrun", where)
	}

	// Files are kept in Postgres unless a bucket is named.
	if address := a.getenv("WEIR_BLOB_URL"); address != "" {
		if openBucket == nil {
			return errors.New("WEIR_BLOB_URL is set, but this build of weir was made without bucket support (the nobuckets tag)")
		}
		files, err := openBucket(a.ctx, address)
		if err != nil {
			return fmt.Errorf("WEIR_BLOB_URL: %w", err)
		}
		defer files.Close()
		eng.Files = files
	} else {
		eng.Files = blob.NewPostgres(eng.Pool())
	}

	// With an admin key set, every request must carry a key. Without one the
	// API trusts whoever calls it, which is only safe on a trusted network.
	adminKey := a.getenv("WEIR_ADMIN_KEY")
	access := "API keys are required"
	if adminKey == "" && a.getenv("WEIR_REQUIRE_ADMIN_KEY") != "" {
		// A server that anyone can reach must never start open by accident.
		// The container image sets this; a laptop does not.
		return errors.New("WEIR_ADMIN_KEY is not set, and this server is set to refuse to run without one (WEIR_REQUIRE_ADMIN_KEY)")
	}
	if adminKey == "" {
		access = "WARNING: no WEIR_ADMIN_KEY is set, so anyone who can reach this API can do anything"
	}
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: api.New(eng, a.reg, a.apiOptions(adminKey)...), ReadHeaderTimeout: 10 * time.Second}
	errs := make(chan error, 2)
	working, err := works(a.getenv("WEIR_WORKER"))
	if err != nil {
		return err
	}
	if working {
		go func() { errs <- w.Run(a.ctx) }()
	} else {
		agents = "by another server: this one only answers the API (WEIR_WORKER=off)"
	}
	// Hear from Postgres when a run moves, so that streams send at once.
	go func() { _ = eng.Listen(a.ctx) }()
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()
	fmt.Fprintf(a.out, "weir is listening on %s with the %s runner; decisions use %s unless a step names a provider\n", cfg.HTTPAddr, cfg.Runner, decideWith)
	fmt.Fprintln(a.out, access)
	fmt.Fprintf(a.out, "files are kept in %s\n", eng.Files.Describe())
	fmt.Fprintf(a.out, "agents run %s\n", agents)

	select {
	case <-a.ctx.Done():
	case err = <-errs:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	return err
}

// agent runs one agent's job and exits. It is started inside an agent's own
// container, not by a person. It talks only to Weir's API, with the token for
// its own step, and gives the agent every tool: the container is the boundary.
func (a *app) agent([]string) error {
	url, token := a.getenv("WEIR_URL"), a.getenv("WEIR_AGENT_TOKEN")
	if url == "" || token == "" {
		return errors.New("agent is started in an agent's container, which is given WEIR_URL and WEIR_AGENT_TOKEN")
	}
	// The agent's command line is given this container's environment, apart
	// from Weir's own variables. There is nothing else here to keep from it.
	var pass []string
	for _, kv := range os.Environ() {
		if name, _, _ := strings.Cut(kv, "="); !strings.HasPrefix(name, "WEIR_") {
			pass = append(pass, name)
		}
	}
	return agent.Run(a.ctx, &agent.Client{BaseURL: url, Token: token}, &runner.Claude{Unrestricted: true, Env: pass})
}

// mcp is started by an agent's command line, not by a person. It serves MCP
// over standard input and output and passes every tool call to Weir's API.
func (a *app) mcp([]string) error {
	url, token := a.getenv("WEIR_TOOL_URL"), a.getenv("WEIR_TOOL_TOKEN")
	if url == "" || token == "" {
		return errors.New("mcp is started by the agent runner, which sets WEIR_TOOL_URL and WEIR_TOOL_TOKEN")
	}
	b := &mcpbridge.Bridge{BaseURL: strings.TrimRight(url, "/"), Token: token}
	return b.Serve(a.ctx, a.in, a.out)
}

func (a *app) publish(args []string) error {
	fs, who := a.flags("publish")
	pos, err := parse(fs, args, 1)
	if err != nil {
		return err
	}
	source, err := os.ReadFile(pos[0])
	if err != nil {
		return err
	}
	eng, _, done, err := a.engine()
	if err != nil {
		return err
	}
	defer done()
	out, err := eng.Publish(a.ctx, source, *who)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "published %s/%s version %d\n", out.Namespace, out.Name, out.Version)
	if len(out.Unsupported) > 0 {
		fmt.Fprintf(a.out, "note: these parts were accepted but are not enforced yet: %s\n", strings.Join(out.Unsupported, ", "))
	}
	return nil
}

func (a *app) template(args []string) error {
	fs, who := a.flags("template")
	user := fs.String("user", "", "the prompt; {{name}} is filled from the step's input, {{input}} with all of it")
	system := fs.String("system", "", "the system prompt")
	pos, err := parse(fs, args, 1)
	if err != nil {
		return err
	}
	if *user == "" {
		return errors.New("a template needs --user <text>")
	}
	eng, _, done, err := a.engine()
	if err != nil {
		return err
	}
	defer done()
	v, err := eng.PublishTemplate(a.ctx, pos[0], engine.Template{System: *system, User: *user}, *who)
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "published %s@%d\n", pos[0], v)
	return nil
}

func (a *app) connection(args []string) error {
	fs, _ := a.flags("connection")
	workspace := fs.String("workspace", "default", "the workspace the connection belongs to")
	kind := fs.String("auth", "", "how to sign in: bearer, header, basic or github-app")
	secret := fs.String("secret", "", "the secret to sign in with, by reference: env:NAME or file:name")
	header := fs.String("header", "", "the header to send, for --auth header")
	username := fs.String("username", "", "the user name, for --auth basic")
	app := fs.String("app", "", "the GitHub App's client id or App id, for --auth github-app")
	mcp := fs.Bool("mcp", false, "the address is an MCP server, whose tools join the workspace")
	readOnly := fs.Bool("read-only", false, "with --mcp: treat every tool as a read")
	hookKind := fs.String("webhook", "", "receive webhooks signed this way: github or hmac (none switches it off)")
	hookSecret := fs.String("webhook-secret", "", "the shared secret the sender signs with, by reference")
	hookHeader := fs.String("webhook-header", "", "with --webhook hmac: the header that carries the signature")
	hookEvent := fs.String("webhook-event-header", "", "with --webhook hmac: the header that says what happened")
	pos, err := parse(fs, args, 2)
	if err != nil {
		return err
	}
	root, _, done, err := a.engine()
	if err != nil {
		return err
	}
	defer done()
	eng, err := root.In(a.ctx, *workspace)
	if err != nil {
		return err
	}
	auth := engine.Auth{Kind: *kind, Secret: *secret, Header: *header, Username: *username, App: *app}
	if *mcp {
		// Ask the server what it offers now, signed in as the connection.
		to := engine.Endpoint{Connection: pos[0], URL: pos[1], Workspace: *workspace, Auth: auth}
		if err := auth.Check(); err != nil {
			return err
		}
		tools, err := worker.DiscoverMCP(a.ctx, a.reg, to)
		if err != nil {
			return fmt.Errorf("the MCP server could not be asked for its tools: %w", err)
		}
		if err := eng.PutMCPConnection(a.ctx, pos[0], pos[1], *readOnly, auth, tools); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "connection %s points at the MCP server %s, which offers %d tool(s)\n", pos[0], pos[1], len(tools))
	} else {
		if err := eng.PutConnection(a.ctx, pos[0], pos[1], auth); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "connection %s points at %s\n", pos[0], pos[1])
	}
	if *hookKind != "" {
		hook := engine.Webhook{Kind: *hookKind, Secret: *hookSecret, Header: *hookHeader, EventHeader: *hookEvent}
		if err := eng.SetWebhook(a.ctx, pos[0], hook); err != nil {
			return err
		}
		if hook.Kind == engine.WebhookNone {
			fmt.Fprintf(a.out, "it no longer receives webhooks\n")
		} else {
			found, readFrom := a.reg.SecretStatus(*workspace, hook.Secret)
			state := "is set"
			if !found {
				state = "is NOT set yet; webhooks will be refused until it is"
			}
			fmt.Fprintf(a.out, "it receives %s webhooks at /v1/webhooks/%s/%s, checked with %s, read from %s, which %s\n",
				hook.Kind, *workspace, pos[0], hook.Secret, readFrom, state)
		}
	}
	if auth.Secret != "" {
		// The secret is never given to Weir: say where the server reads it from.
		found, readFrom := a.reg.SecretStatus(*workspace, auth.Secret)
		state := "is set"
		if !found {
			state = "is NOT set yet; calls through this connection will fail until it is"
		}
		fmt.Fprintf(a.out, "it signs in with %s, read from %s, which %s\n", auth.Secret, readFrom, state)
	}
	return nil
}

func (a *app) start(args []string) error {
	fs, who := a.flags("run")
	input := fs.String("input", "{}", "the run's input as JSON")
	key := fs.String("key", "", "an idempotency key")
	pos, err := parse(fs, args, 1)
	if err != nil {
		return err
	}
	namespace, name, ok := strings.Cut(pos[0], "/")
	if !ok {
		return fmt.Errorf("name the workflow as <namespace>/<name>, not %q", pos[0])
	}
	if !json.Valid([]byte(*input)) {
		return errors.New("--input is not valid JSON")
	}
	eng, _, done, err := a.engine()
	if err != nil {
		return err
	}
	defer done()
	id, created, err := eng.StartRun(a.ctx, engine.StartRequest{Namespace: namespace, Name: name,
		Input: json.RawMessage(*input), Requester: *who, IdempotencyKey: *key})
	if err != nil {
		return err
	}
	if !created {
		fmt.Fprintf(a.out, "%s (already started with this key)\n", id)
		return nil
	}
	fmt.Fprintln(a.out, id)
	return nil
}

func (a *app) status(args []string) error {
	fs, _ := a.flags("status")
	events := fs.Bool("events", false, "show the event log")
	pos, err := parse(fs, args, 1)
	if err != nil {
		return err
	}
	eng, _, done, err := a.engine()
	if err != nil {
		return err
	}
	defer done()
	v, err := eng.Run(a.ctx, pos[0])
	if err != nil {
		return err
	}
	fmt.Fprintf(a.out, "run %s: %s, spent $%.4f\n", v.ID, v.Status, v.SpentUSD)
	if len(v.Error) > 0 {
		fmt.Fprintf(a.out, "error: %s\n", v.Error)
	}
	tw := tabwriter.NewWriter(a.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "\nSTEP\tKIND\tSTATUS\tATTEMPTS\tRESULT")
	for _, s := range v.Steps {
		result := s.Label + s.Outcome
		if result == "" && len(s.Error) > 0 {
			result = string(s.Error)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n", s.Key, s.Kind, s.Status, s.Attempts, result)
	}
	tw.Flush()
	if *events {
		fmt.Fprintln(tw, "\n#\tEVENT\tSTEP\tBY")
		for _, e := range v.Events {
			fmt.Fprintf(tw, "%d\t%s\t%s\t%s\n", e.Seq, e.Type, e.Step, e.Actor)
		}
		tw.Flush()
	}
	return nil
}

func (a *app) tasks([]string) error {
	eng, _, done, err := a.engine()
	if err != nil {
		return err
	}
	defer done()
	tasks, err := eng.Tasks(a.ctx)
	if err != nil {
		return err
	}
	if len(tasks) == 0 {
		fmt.Fprintln(a.out, "nothing is waiting for a person")
		return nil
	}
	tw := tabwriter.NewWriter(a.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "TASK\tRUN\tSTEP\tFOR\tOUTCOMES")
	for _, t := range tasks {
		who := t.Role
		if t.User != "" {
			who = t.User
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", t.ID, t.RunID, t.Step, who, strings.Join(t.Outcomes, ", "))
	}
	return tw.Flush()
}

func (a *app) approve(args []string) error {
	fs, who := a.flags("approve")
	outcome := fs.String("outcome", "approved", "the decision")
	note := fs.String("note", "", "a note to record with the decision")
	pos, err := parse(fs, args, 1)
	if err != nil {
		return err
	}
	eng, _, done, err := a.engine()
	if err != nil {
		return err
	}
	defer done()
	if err := eng.Decide(a.ctx, pos[0], *who, *outcome, *note); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "recorded %q by %s\n", *outcome, *who)
	return nil
}

func (a *app) resume(args []string) error {
	fs, who := a.flags("resume")
	budget := fs.Float64("budget", 0, "a new budget in USD")
	pos, err := parse(fs, args, 1)
	if err != nil {
		return err
	}
	eng, _, done, err := a.engine()
	if err != nil {
		return err
	}
	defer done()
	if err := eng.Resume(a.ctx, pos[0], *who, *budget); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "resumed %s\n", pos[0])
	return nil
}

// names splits a comma-separated list, dropping blanks.
func names(list string) []string {
	var out []string
	for _, item := range strings.Split(list, ",") {
		if item = strings.TrimSpace(item); item != "" {
			out = append(out, item)
		}
	}
	return out
}

// pairs reads "a=b,c=d" into a map, ignoring anything that is not a pair.
func pairs(list string) map[string]string {
	out := map[string]string{}
	for _, item := range names(list) {
		if key, value, ok := strings.Cut(item, "="); ok && strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			out[strings.TrimSpace(key)] = strings.TrimSpace(value)
		}
	}
	return out
}

// key manages API keys. It works on the database directly, so that the first
// key can be made before any key exists.
func (a *app) key(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: weir key create <name> | list | revoke <key-id>")
	}
	fs, who := a.flags("key " + args[0])
	workspace := fs.String("workspace", "default", "the workspace the key belongs to")
	role := fs.String("role", engine.RoleWriter, "reader, writer or admin")
	actAs := fs.Bool("act-as", false, "let the key name the person it is acting for")
	want := map[string]int{"create": 1, "list": 0, "revoke": 1}
	n, ok := want[args[0]]
	if !ok {
		return fmt.Errorf("unknown key command %q", args[0])
	}
	pos, err := parse(fs, args[1:], n)
	if err != nil {
		return err
	}
	root, _, done, err := a.engine()
	if err != nil {
		return err
	}
	defer done()
	eng, err := root.In(a.ctx, *workspace)
	if err != nil {
		return err
	}
	switch args[0] {
	case "create":
		secret, key, err := eng.CreateKey(a.ctx, pos[0], *role, *actAs, *who)
		if err != nil {
			return err
		}
		fmt.Fprintf(a.out, "key %s for workspace %s (%s)\n\n  %s\n\nThis is the only time it is shown. Send it as: Authorization: Bearer <key>\n",
			key.Name, key.Workspace, key.Role, secret)
	case "list":
		keys, err := eng.Keys(a.ctx)
		if err != nil {
			return err
		}
		for _, k := range keys {
			state := "active"
			if k.RevokedAt != nil {
				state = "revoked"
			}
			fmt.Fprintf(a.out, "%s  %-20s %-7s %s\n", k.ID, k.Name, k.Role, state)
		}
	case "revoke":
		if err := eng.RevokeKey(a.ctx, pos[0]); err != nil {
			return err
		}
		fmt.Fprintf(a.out, "key %s no longer works\n", pos[0])
	}
	return nil
}

// apiOptions reads how the API is to be reached: with which key, behind which
// proxy, and whether it serves the console as well.
func (a *app) apiOptions(adminKey string) []api.Option {
	opts := []api.Option{api.WithAdminKey(adminKey)}
	if dir := a.getenv("WEIR_CONSOLE_DIR"); dir != "" {
		opts = append(opts, api.WithConsole(dir))
	}
	if header := a.getenv("WEIR_PROXY_USER_HEADER"); header != "" {
		opts = append(opts, api.WithProxyUser(header))
	}
	if prefix := a.getenv("WEIR_API_PREFIX"); prefix != "" {
		opts = append(opts, api.WithPrefix(prefix))
	}
	return opts
}

// works reads WEIR_WORKER: whether this server does the work as well as
// answering the API. A server that only answers people can leave the work to
// another on the same database, which matters on a host that pauses a server
// between requests: a paused worker loses its database connection.
func works(setting string) (bool, error) {
	switch setting {
	case "", "on":
		return true, nil
	case "off":
		return false, nil
	}
	return false, fmt.Errorf("WEIR_WORKER is %q: it must be on or off", setting)
}
