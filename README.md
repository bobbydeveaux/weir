# Weir

A workflow engine built for AI. Every step is one of five kinds (decide, AI, deterministic, human or external), and all five obey one contract: typed inputs and outputs, an owner, a budget and a full audit record. Weir runs as a stateless Go service on Postgres.

**Status: early build.** The engine runs workflows end to end on a laptop, with all five step kinds. It is not released, the contract will change, and several parts of the design are not built yet (see below). The first tagged version will be `v0.1.0`.

Site: [weir.stackramp.io](https://weir.stackramp.io) · Integration docs: [weir.stackramp.io/docs](https://weir.stackramp.io/docs/) · [What is done and what is left](docs/build/02-epics.md)

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
- **Sign-in for people.** API keys exist (see Access), but a person has no sign-in of their own: the console takes a pasted key, and an approver's identity is whatever a trusted application says it is.
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
| `WEIR_AGENT_ENV` | Names of environment variables to let an agent's command line see, comma-separated. See "Bedrock, Vertex AI and gateways" below | none |
| `WEIR_CLAUDE_MODELS` | The model for each tier, as `small=…,large=…` | `small=haiku,large=opus` |
| `WEIR_CLAUDE_MODEL_PREFIX` | Put in front of a model name that begins `claude-`, such as `eu.anthropic.` for Bedrock | none |
| `WEIR_TYPESAFE_URL`, `WEIR_GEMINI_API_URL` | A different address for Jev or Gemini Decisions, to go through a gateway or proxy | each service's own |
| `WEIR_SECRETS_DIR` | Directory of secret files for `file:` references, with one directory per workspace | none |
| `WEIR_SECRET__<WORKSPACE>__<NAME>` | The value of the secret a connection in that workspace names as `env:<NAME>` | none |
| `WEIR_CODEX_MODEL` | Model for steps with `tier: codex`, which run through the Codex command line | Codex's default |

The `cli` runner keeps the command line's permission checks on, offers it no built-in tools, ignores user and project settings, and passes it a short list of environment variables rather than the worker's own.

### Access

Without `WEIR_ADMIN_KEY`, the API trusts whoever calls it. That is for a laptop. Set it, and every request needs a key:

```bash
export WEIR_ADMIN_KEY=$(openssl rand -hex 32)      # may do anything, in any workspace
weir key create my-app --workspace my-app --role writer --act-as
#   weir_3f9c…    shown once; only a hash is stored
curl $WEIR/v1/runs -H "Authorization: Bearer weir_3f9c…"
```

- A key belongs to one workspace. It needs no `X-Weir-Workspace` header, and naming another workspace is refused.
- Roles: a `reader` reads; a `writer` also publishes, starts, decides, resumes and cancels; an `admin` also manages the workspace's keys. Only the admin key creates workspaces.
- A key acts as itself: its name is the actor on everything it does. A key made with `--act-as` may name the person it is acting for in `X-Weir-Actor`, which is for an application with its own sign-in.
- Callbacks and an agent's tool calls carry a one-time token for a single attempt and need no key.

Not yet: sign-in for people, and signed hooks.

### Credentials for connections

A connection can say how Weir proves who it is to the other side. Weir holds the name of the secret and never its value: the platform Weir runs on supplies that, as an environment variable or a file, which is something Cloud Run, AWS, Azure and Kubernetes can all do.

```bash
# The secret, given to the Weir server by the platform:
export WEIR_SECRET__MY_APP__GITHUB_TOKEN=ghp_…

# The connection, which only names it:
weir connection github https://api.github.com --workspace my-app --auth bearer --secret env:GITHUB_TOKEN
```

| Kind | Sends |
|---|---|
| `bearer` | `Authorization: Bearer <secret>` |
| `header` | `<header>: <secret>`, with `--header X-Api-Key` |
| `basic` | `Authorization: Basic …`, with `--username` |

A secret is named in one of two ways, and a workspace can name only its own:

| Reference | In workspace `my-app`, read from |
|---|---|
| `env:GITHUB_TOKEN` | the variable `WEIR_SECRET__MY_APP__GITHUB_TOKEN` |
| `file:github-token` | the file `$WEIR_SECRETS_DIR/my-app/github-token` |

- The reference is a short name, not a variable or a path. A workspace cannot name the server's own settings, a file elsewhere on the machine, or another workspace's secret.
- The credential is added to every call made through the connection: an external step, a document fetched for an agent, an agent's tool call, a hook, and an MCP server. An agent names a connection and never sees the credential.
- The value is read when the call is made. It is not written to the database, an event, a log or any reply.
- If the secret is not there, the call is not made. The step fails with a message naming the variable or file to set, and the run waits to be resumed.
- A redirect to a different host is not followed, so a third party cannot send the credential somewhere else.

Not yet: signing in with the machine's own cloud identity, with no secret at all, and limiting which addresses a connection may point at.

### Webhooks from a third party

A connection can also receive. The third party posts to an address on Weir when something happens, Weir checks the request really came from them, and it becomes an event that a workflow's trigger can start on.

```bash
export WEIR_SECRET__MY_APP__GH_WEBHOOK=…          # the same secret you give GitHub
weir connection github https://api.github.com --workspace my-app \
  --webhook github --webhook-secret env:GH_WEBHOOK
# it receives github webhooks at /v1/webhooks/my-app/github
```

In GitHub, set the webhook's payload address to `https://<your weir>/v1/webhooks/my-app/github`, its content type to `application/json`, and its secret to the same value. A workflow then starts on it:

```yaml
triggers:
  - event: github.pull_request          # the connection's name, then what happened
    when: event.data.action == "opened"
```

- The sender cannot send Weir's headers or hold an API key, so the workspace and the connection are in the address and **the signature is the credential**. A request with a missing, wrong or tampered signature is refused and nothing is recorded.
- `--webhook github` checks GitHub's `X-Hub-Signature-256`. `--webhook hmac` is for any sender that signs the body with HMAC-SHA256: name its headers with `--webhook-header` and `--webhook-event-header`.
- The sender's delivery id is kept, so a delivery sent twice starts nothing new.
- The shared secret is named by reference and never stored, like a connection's credential. One workspace's secret does not verify another's webhooks.

Not yet: Slack's signature scheme, and refusing an old request that is replayed without a delivery id.

### GitHub tools

An agent can read a repository and propose a change to it. The tools go through a connection, so Weir adds the token, checks each call against the step's list, and records it. The agent never sees the token.

```bash
export WEIR_SECRET__MY_APP__GH_TOKEN=…
weir connection github https://api.github.com/repos/acme/app --workspace my-app --auth bearer --secret env:GH_TOKEN
```

```yaml
- id: fix
  kind: ai
  prompt: fix-the-bug@1
  tools:
    read: [github.get_file, github.list_files, github.get_pull_request]
    write: [github.create_branch, github.put_file, github.open_pull_request, github.comment]
  budget: { usd: 2.00 }
```

| Tool | Class | Does |
|---|---|---|
| `github.get_file` | read | Reads one text file, up to 200 KB |
| `github.list_files` | read | Lists a directory |
| `github.get_pull_request` | read | A pull request's title, description, branches and changed files |
| `github.create_branch` | write | Creates a branch |
| `github.put_file` | write | Creates or replaces one file on a branch, as a commit |
| `github.open_pull_request` | write | Opens a pull request from a branch |
| `github.comment` | write | Comments on an issue or a pull request |

- **Pin a connection to one repository** by giving it the address `https://api.github.com/repos/<owner>/<name>`, as above. The tools then work on that repository only. An agent that has been talked into something cannot reach another repository the token happens to cover. A connection to `https://api.github.com` reaches any repository the token allows, and each call must name one.
- **An agent cannot write to the default branch.** `github.put_file` refuses it. The way in is a branch and a pull request, which a person or a check can turn down.
- **Writes are safe to repeat.** A branch or pull request that already exists is returned, and writing the same content again changes nothing, so a retried step does not fail for having worked the first time.
- Give the token only what the workflow needs. A fine-grained token for one repository, with contents and pull requests, is enough for all seven tools.

With a webhook on the same connection (above), a pull request can start a workflow whose agent reads it and replies.

The read tools have been run against the real GitHub. The write tools have been tested against a stand-in only.

### Bedrock, Vertex AI and gateways

An agent's command line is started with almost none of the server's environment, so that nothing on the machine can change how an agent behaves. To send Claude somewhere other than Anthropic's own API, name the variables to let through.

Claude on Amazon Bedrock, signed in with the machine's own IAM role:

```bash
export CLAUDE_CODE_USE_BEDROCK=1 AWS_REGION=eu-west-1
export WEIR_AGENT_ENV=CLAUDE_CODE_USE_BEDROCK,AWS_REGION,AWS_ROLE_ARN,AWS_WEB_IDENTITY_TOKEN_FILE,AWS_CONTAINER_CREDENTIALS_RELATIVE_URI
export WEIR_CLAUDE_MODEL_PREFIX=eu.anthropic.
export WEIR_CLAUDE_MODELS=small=claude-haiku-5-5,large=claude-opus-5-5
```

Claude on Vertex AI, signed in through Workload Identity Federation:

```bash
export CLAUDE_CODE_USE_VERTEX=1 ANTHROPIC_VERTEX_PROJECT_ID=my-project CLOUD_ML_REGION=europe-west1
export WEIR_AGENT_ENV=CLAUDE_CODE_USE_VERTEX,ANTHROPIC_VERTEX_PROJECT_ID,CLOUD_ML_REGION,GOOGLE_APPLICATION_CREDENTIALS
```

Through an AI gateway:

```bash
export ANTHROPIC_BASE_URL=https://gateway.internal ANTHROPIC_AUTH_TOKEN=…
export WEIR_AGENT_ENV=ANTHROPIC_BASE_URL,ANTHROPIC_AUTH_TOKEN
```

List the credential variables your platform actually uses; the ones above are the common ones. None of these three has been run against a real account yet. In particular, check that a cost is still recorded for each call: budgets depend on it.

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
