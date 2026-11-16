// Command weir is the engine's command line.
package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/bobbydeveaux/weir/engine/store"
	"github.com/bobbydeveaux/weir/internal/config"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

const usage = `weir is a workflow engine built for AI.

Usage:
  weir <command>

Commands:
  migrate   Bring the database up to the latest schema
  version   Print the version

Environment:
  WEIR_DATABASE_URL  Postgres connection string (required by migrate)
  WEIR_RUNNER        How ai and decide steps run: cli, api or fake (default cli)
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run executes one command and returns the process exit code.
func run(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}
	switch args[0] {
	case "version":
		fmt.Fprintln(stdout, version)
		return 0
	case "migrate":
		cfg, err := config.FromEnv(getenv)
		if err != nil {
			fmt.Fprintln(stderr, "weir:", err)
			return 1
		}
		v, err := store.Migrate(ctx, cfg.DatabaseURL)
		if err != nil {
			fmt.Fprintln(stderr, "weir:", err)
			return 1
		}
		fmt.Fprintf(stdout, "schema is at version %d\n", v)
		return 0
	case "help", "-h", "--help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "weir: unknown command %q\n\n%s", args[0], usage)
		return 2
	}
}
