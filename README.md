# Weir

A workflow engine built for AI. Every step is one of five kinds (decide, AI, deterministic, human or external), and all five obey one contract: typed inputs and outputs, an owner, a budget and a full audit record. Weir runs as a stateless Go service on Postgres.

**Status: early build.** The engine runs workflows end to end on a laptop, with all five step kinds. It is not released, the contract will change, and several parts of the design are not built yet (see below). The first tagged version will be `v0.1.0`.

Site: [weir.stackramp.io](https://weir.stackramp.io)

## Try it

You need Go 1.26 or newer, Docker, and the Claude command line signed in.

```sh
make up && make build
export WEIR_DATABASE_URL='postgres://weir:weir@localhost:54329/weir?sslmode=disable'
./bin/weir migrate
./bin/weir serve
```

Then follow [the support inbox example](examples/support-inbox), which classifies a message, drafts a reply, waits for a person to approve it, and sends it.

## What works today

| Area | What is built |
| --- | --- |
| Definitions | YAML workflows, checked when published; immutable versions; a `live` alias |
| Expressions | CEL, for `next` rules, input mappings and poll conditions |
| `deterministic` steps | Registered handlers, retries with backoff, an idempotency key per step |
| `decide` steps | One label from a closed list; an unsure, wrong or failed answer becomes the default |
| `ai` steps | A pinned prompt, output checked against a JSON Schema by the engine, and a budget |
| `human` steps | An inbox, outcomes, timeouts, escalation, and a requester who cannot approve their own run |
| `external` steps | Call a third party, then resume on a callback with a one-time token, or poll until a condition holds |
| Failure | `block`, `cancel` and `continue`; undo handlers run in reverse on `cancel`; a blocked run can be resumed |
| Tokens | One per attempt, stored hashed. A late, repeated or made-up completion changes nothing |
| Crashes | A worker that disappears loses its lease, and the step is run again |
| Audit | An append-only event log per run, and a record of every model call with its template, tokens and cost |
| Runners | `cli` runs steps through the Claude command line; `fake` returns scripted answers for tests |
| Interfaces | An HTTP API and the `weir` command line |

The database enforces the rules that matter: published versions and events cannot be changed or deleted, and a step can have only one live attempt.

## What is not built yet

- **Tools for agents.** An `ai` step can reason and return structured output, but cannot read or change anything. The tool proxy is next.
- **Triggers, hooks, tiers and sandbox settings.** A definition may contain them, and publishing says they are not enforced.
- **Authentication.** The API trusts the `X-Weir-Actor` header. Run it only where every caller is trusted.
- **Roles.** A human step records the role it is for, but does not yet check that the person deciding holds it. Only "not the requester" and a named user are enforced.
- **Transcripts.** Model calls are recorded with their cost and usage, but the full transcript is not stored yet.
- **Other providers.** Codex and Gemini adapters, and the `api` runner.
- **Parallel steps, fan-out and sub-workflows.** Steps run one at a time.
- **Scale to zero.** Workers poll. The wake-up path is not built.
- **CI has not run.** The workflow is written, but the repository is not on GitHub yet.

## Develop

```sh
make up        # start Postgres on localhost:54329
make test      # tests that need no database
make test-db   # every test, against Postgres
make check     # what CI runs
```

Run `make help` for the full list. Tests for the engine run against a real Postgres, each in its own schema.

To make one real call through the Claude command line: `WEIR_TEST_CLAUDE=1 go test -run TestClaudeLive ./runner/`.

## Configuration

| Variable | Meaning | Default |
| --- | --- | --- |
| `WEIR_DATABASE_URL` | Postgres connection string | required |
| `WEIR_HTTP_ADDR` | Address the API listens on | `:8080` |
| `WEIR_PUBLIC_URL` | Address third parties use to reach the API | `http://localhost` plus the port |
| `WEIR_RUNNER` | How `ai` and `decide` steps run: `cli`, `api` or `fake` | `cli` |
| `WEIR_TYPESAFE_KEY` | API key for Jev. When set, `decide` steps use Jev unless they name another provider | none |
| `WEIR_GEMINI_API_KEY` | API key for Gemini Decisions, for `decide` steps with `provider: gemini-decision` | none |
| `WEIR_DECIDE_PROVIDER` | Provider for `decide` steps that name none: `jev`, `gemini-decision`, `claude` or `codex` | `jev` with a key, otherwise `claude` |
| `WEIR_CODEX_MODEL` | Model for steps with `tier: codex`, which run through the Codex command line | Codex's default |

The `cli` runner keeps the command line's permission checks on, offers it no built-in tools, ignores user and project settings, and passes it a short list of environment variables rather than the worker's own.

## Layout

| Path | Holds |
| --- | --- |
| `spec/` | The contract: parsing, the publish rules, expressions |
| `engine/` | The engine. Every state change is one transaction |
| `engine/store/` | The Postgres schema and migrations |
| `worker/` | Claims steps and does the work |
| `runner/` | How models are reached: the Claude command line, or a scripted fake |
| `api/` | The HTTP API |
| `cmd/weir/` | The command line |
| `examples/` | Workflows you can run |

## Licence

Apache 2.0. See [LICENSE](LICENSE).
