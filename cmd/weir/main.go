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

	"github.com/bobbydeveaux/weir/api"
	"github.com/bobbydeveaux/weir/engine"
	"github.com/bobbydeveaux/weir/engine/store"
	"github.com/bobbydeveaux/weir/internal/config"
	"github.com/bobbydeveaux/weir/runner"
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

Running work:
  run <namespace>/<name>           Start a run            [--input <json>] [--key <idempotency-key>]
  status <run-id>                  Show a run             [--events]
  tasks                            List steps waiting for a person
  approve <task-id>                Decide a waiting step  [--outcome approved] [--note <text>]
  resume <run-id>                  Restart a blocked run  [--budget <usd>]

Other:
  version                          Print the version

Most commands take --as <name> to say who is acting. It defaults to $USER.

Environment:
  WEIR_DATABASE_URL  Postgres connection string (required)
  WEIR_HTTP_ADDR     Address the API listens on (default :8080)
  WEIR_PUBLIC_URL    Address third parties use to reach the API (default http://localhost:8080)
  WEIR_RUNNER        How ai and decide steps run: cli, api or fake (default cli)
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// app is one invocation of the command line.
type app struct {
	ctx    context.Context
	getenv func(string) string
	out    io.Writer
}

// run executes one command and returns the process exit code.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	a := &app{ctx: ctx, getenv: getenv, out: stdout}
	commands := map[string]func([]string) error{
		"migrate": a.migrate, "serve": a.serve, "publish": a.publish, "template": a.template,
		"connection": a.connection, "run": a.start, "status": a.status, "tasks": a.tasks,
		"approve": a.approve, "resume": a.resume,
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
	// Refuse workflows that name a handler no worker can run.
	eng.HandlerExists = worker.NewRegistry().Exists
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
	switch cfg.Runner {
	case config.RunnerCLI:
		r = &runner.Claude{Models: map[string]string{"small": "haiku", "large": "opus"}}
	case config.RunnerFake:
		r = &runner.Fake{}
	default:
		return fmt.Errorf("the %s runner: %w", cfg.Runner, runner.ErrNotBuilt)
	}
	public := a.getenv("WEIR_PUBLIC_URL")
	if public == "" {
		public = "http://localhost" + cfg.HTTPAddr
	}
	w := &worker.Worker{Engine: eng, Runner: r, Handlers: worker.NewRegistry(), ID: "weir-serve", CallbackURL: public}

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: api.New(eng), ReadHeaderTimeout: 10 * time.Second}
	errs := make(chan error, 2)
	go func() { errs <- w.Run(a.ctx) }()
	go func() {
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			errs <- err
		}
	}()
	fmt.Fprintf(a.out, "weir is listening on %s with the %s runner\n", cfg.HTTPAddr, cfg.Runner)

	select {
	case <-a.ctx.Done():
	case err = <-errs:
	}
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdown)
	return err
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
	pos, err := parse(fs, args, 2)
	if err != nil {
		return err
	}
	eng, _, done, err := a.engine()
	if err != nil {
		return err
	}
	defer done()
	if err := eng.PutConnection(a.ctx, pos[0], pos[1]); err != nil {
		return err
	}
	fmt.Fprintf(a.out, "connection %s points at %s\n", pos[0], pos[1])
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
