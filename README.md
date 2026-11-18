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
| Expressions | CEL, for `next` rules, input mappings, triggers and poll conditions |
| `deterministic` steps | Registered handlers, retries with backoff, an idempotency key per step |
| `decide` steps | One label from a closed list; an unsure, wrong or failed answer becomes the default. Answered by a small model or a decision model (Jev, Gemini Decisions), chosen per step |
| `ai` steps | A pinned prompt, a budget, output checked against a JSON Schema, documents the agent may read, files it may write, a prepared working directory, a sandboxed shell, and tools |
| `human` steps | An inbox, outcomes, timeouts, escalation, values sent with a decision, and a requester who cannot approve their own run |
| `external` steps | Call a third party, then resume on a callback with a one-time token, or poll until a condition holds |
| Tools for agents | Every call goes through a proxy that checks it against the step's list and records it. Tools come from the server, from a workspace's MCP servers, and for GitHub |
| Connections | A named address per workspace, HTTP or MCP, with a credential named by reference and never stored |
| Events in | An application reports what happened, or a third party posts a signed webhook; triggers start the workflows that want it. A workflow can also start on a schedule |
| Events out | Hooks to an application, signed if asked, retried until delivered; and live streams of a run's events |
| Failure | `block`, `cancel` and `continue`; undo handlers run in reverse; a blocked run can be resumed or cancelled |
| Tokens | One per attempt, stored hashed. A late, repeated or made-up completion changes nothing |
| Crashes | A worker that disappears loses its lease, and the step is run again |
| Audit | An append-only event log per run, and a record of every model call (prompt, reply, tokens, cost) and every tool call |
| Tenancy and access | Workspaces that cannot see each other; API keys with roles, each bound to one workspace |
| Models | Claude and Codex through their command lines; Claude on Bedrock, Vertex AI or through a gateway by settings |
| Interfaces | An HTTP API, the `weir` command line, and a web console in `console/` |

The database enforces the rules that matter: published versions and events cannot be changed or deleted, and a step can have only one live attempt.

A real application runs on it: [how it is wired](docs/build/04-agent-demo-on-weir.md).

## What is not built yet

The full list, with an issue for each item, is [docs/build/02-epics.md](docs/build/02-epics.md). The larger gaps:

- **Sign-in for people.** There are API keys (see Access), but a person has no sign-in of their own: the console takes a pasted key, and an approver's identity is whatever a trusted application says it is.
- **Roles on approvals.** A human step records the role it is for, but does not yet check that the person deciding holds it. Only "not the requester" and a named user are enforced.
- **Scale to zero.** A worker polls for work, so one instance must stay running.
- **Calling models directly.** Agents run through a command line. A runner that calls a provider's API itself, for Bedrock, Vertex AI and Azure AI Foundry beyond Claude, is not built.
- **Running an agent in the cloud.** An agent can run in a container of its own with Docker. Launchers for Cloud Run, Fargate and AgentCore are not built.
- **Pipes as packages.** The parts exist (connections, webhooks, tools), and GitHub has all three, but there is no installable package or catalogue.
- **Parallel steps, fan-out and sub-workflows.** Steps run one at a time.
- **Tiers and sandbox settings in a definition.** They parse, and publishing says they are not enforced.
- **A tagged release.** There is no version yet, and the contract will change before `v0.1.0`.

Some of what is built has not met the real service it is for: the write tools for GitHub, the decision models, and Bedrock, Vertex AI and gateways have been tested against stand-ins or not run at all. The to-do list says which, item by item.

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
| `WEIR_BLOB_URL` | Where files are kept: `s3://bucket?region=…`, `gs://bucket`, `azblob://container` or `file:///directory`. See "Where files are kept" below | in Postgres |
| `WEIR_DATABASE_HOST`, `_PORT`, `_NAME`, `_USER`, `_PASSWORD`, `_SSLMODE` | The database in parts, for a platform that hands them out one at a time. Used when `WEIR_DATABASE_URL` is not set | none |
| `PORT` | The port to listen on, where a platform sets it and `WEIR_HTTP_ADDR` is not set | none |
| `WEIR_REQUIRE_ADMIN_KEY` | Refuse to start without `WEIR_ADMIN_KEY`. The container image sets it | not set |
| `WEIR_AGENT_RUNNER` | Where agents run: `inline` (beside the worker) or `docker` (a container each). See "Where agents run" below | `inline` |
| `WEIR_AGENT_IMAGE` | The image agents run in, for `docker`. Build it from `deploy/agent.Dockerfile` | none |
| `WEIR_AGENT_URL` | Weir's address as an agent's container reaches it | `http://host.docker.internal` and the API's port |
| `WEIR_AGENT_DOCKER_ARGS` | Extra arguments for `docker run`: a network, memory and CPU limits | none |
| `WEIR_AGENT_SHELL` | `on` lets a step give its agent a sandboxed shell. See "A shell for an agent" below | off |
| `WEIR_SECRETS_DIR` | Directory of secret files for `file:` references, with one directory per workspace | none |
| `WEIR_SECRET__<WORKSPACE>__<NAME>` | The value of the secret a connection in that workspace names as `env:<NAME>` | none |
| `WEIR_CODEX_MODEL` | Model for steps with `tier: codex`, which run through the Codex command line | Codex's default |

The `cli` runner keeps the command line's permission checks on, offers it no built-in tools, ignores user and project settings, and passes it a short list of environment variables rather than the worker's own.

### In a container

```bash
docker compose --profile weir up        # Postgres and Weir, built from this checkout
curl localhost:8080/v1/workspaces -H 'Authorization: Bearer local-admin-key'
```

Two images are published: `ghcr.io/bobbydeveaux/weir` (the server) and `ghcr.io/bobbydeveaux/weir-agent` (what an agent runs in). `edge` is the latest commit on `main`. The server image refuses to start without `WEIR_ADMIN_KEY`, so that a server on a network is never open by accident.

To run Weir on a platform of your own, start from the image in a small repository of your own: [docs/deploy/your-own-platform.md](docs/deploy/your-own-platform.md).

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

Not yet: sign-in for people.

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
| `github-app` | Nothing. The secret is a GitHub App's private key, which signs requests for short-lived tokens. See "Signing in as a GitHub App" |

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

### Signing in as a GitHub App

A GitHub App is the right credential for a server. Installing it on an organisation is the organisation saying what Weir may do, and its tokens last an hour.

```bash
# The App's private key, given to the Weir server by the platform. Weir never stores it.
mkdir -p $WEIR_SECRETS_DIR/my-app && cp fixer.private-key.pem $WEIR_SECRETS_DIR/my-app/github-app-key

# The connection names the App and the key.
weir connection github https://api.github.com --workspace my-app \
  --auth github-app --app Iv23liExampleClientId --secret file:github-app-key

# Ask Weir whether it can sign in. It answers with the App's name and where it is installed.
curl -X POST $WEIR/v1/connections/github/check -H 'X-Weir-Workspace: my-app'
```

- `--app` is the App's client id or App id, from its settings page on GitHub.
- For each call, Weir asks GitHub for a token for **one repository** with the **least permission the call needs**: reading a file asks for read, opening a pull request asks for write on pull requests and nothing else.
- A token is held in memory for ten minutes and never written anywhere. An agent never sees one.
- Only the GitHub tools can use a connection of this kind. An external step or a plain HTTP tool that names it is refused.
- The check never fails for a missing key: it says which variable or file to set.

Proven against the real GitHub: Weir signed in as an App, listed where it is installed, read a private repository with a token minted for that repository alone, pushed a branch and opened a pull request.

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

### Where files are kept

Weir has one place to keep files, for anything too large or too binary to sit in a step's output. The storage behind it is a setting.

| `WEIR_BLOB_URL` | Files go to |
|---|---|
| not set | The Postgres database Weir already uses. Right for a small install and modest files |
| `s3://my-bucket?region=eu-west-1` | Amazon S3 |
| `gs://my-bucket` | Google Cloud Storage |
| `azblob://my-container` | Azure Blob Storage |
| `file:///var/lib/weir/files` | A directory on disk |

Weir holds no key for a bucket. Each cloud's own library finds the credentials the platform provides: an IAM role on AWS, Workload Identity on Google Cloud, a managed identity on Azure. The server says which storage it is using when it starts.

A file is at most 32 MB. One that is larger is refused and nothing of it is kept.

The cloud storage libraries are most of the size of the `weir` program: about 88 MB with them, about 27 MB without. If you will only ever keep files in Postgres, build with `go build -tags nobuckets ./cmd/weir`.

The same tests run against every kind of storage. They pass for Postgres, for a directory, and for S3 through the AWS library against an S3-compatible server. Google Cloud Storage and Azure have not been run.

### Files an agent writes

An `ai` step with `writes:` may produce files. Weir keeps them in the file store, and a later step or your application collects them.

```yaml
steps:
  - id: draft
    kind: ai
    prompt: draft-change@3
    writes: { max_files: 20, max_megabytes: 8 }   # these are the defaults; "writes: {}" is enough
    output: { schema: { type: object } }
    budget: { usd: 2 }

  - id: review                     # another agent is given everything draft wrote
    kind: ai
    prompt: review-change@1
    files:
      - step: draft
    output: { schema: { type: object } }
    budget: { usd: 1 }

  - id: commit                     # your code is told what was written, and fetches it
    kind: external
    connection: my-app
    call: { path: /commit }
    input: { run: run.id, files: steps.draft.files }
```

- The agent is given one directory, `out`, and can write nowhere else. Where files go inside it is up to your prompt.
- `steps.<id>.files` is a list of `path`, `size`, `sha256` and `content_type`. It holds no contents.
- Your application lists a run's files with `GET /v1/runs/{id}/files` and downloads one with `GET /v1/runs/{id}/files/{step}/{path}`. Both are scoped to the workspace. A download is always sent as an attachment, with its digest in `Weir-File-Sha256`.
- A step that is visited again replaces what its earlier visit wrote.
- Going over a limit fails the step, and nothing it wrote is kept. The most a definition may ask for is 200 files and 64 MB; a single file is at most 32 MB.
- What is not built: the Codex adapter cannot write files, and nothing removes old files yet.

### Where agents run

An agent that can do real work should run in a container of its own, with every tool, and with nothing in reach worth stealing. Weir can run agents two ways:

| `WEIR_AGENT_RUNNER` | The agent runs | It can |
|---|---|---|
| `inline` (the default) | beside Weir's worker, on the same machine | use only what its step lists, inside a sandbox |
| `docker` | in a container of its own, one per step | use every tool. The container is the boundary |

```bash
docker build -f deploy/agent.Dockerfile -t weir-agent .
export WEIR_AGENT_RUNNER=docker WEIR_AGENT_IMAGE=weir-agent
export CLAUDE_CODE_OAUTH_TOKEN=…        # from "claude setup-token"; passed to the container
weir serve
```

**What the container is given:** Weir's address, the one-time token for its own step, and the credential for the model. Nothing else. It holds no Weir secret, no database address and no third party's credential, so there is nothing for a misled agent to send anywhere.

**What happens:**

1. The worker prepares the agent's directory (a checkout, for example), with whatever credential that takes.
2. The container fetches its job and that directory from Weir's API, using its token.
3. The agent works with every tool: a shell, the web, anything in the image.
4. It sends back its output, what the model call cost, and the files it changed.
5. Weir checks all of it as it does for any agent: the output against the schema, the changed paths against the step's rules, the limits, and the budget.

- Tools a step lists still go through Weir, which checks and records each call.
- The token is good for one attempt. Once the step ends it opens nothing.
- What a container may reach on the network is the platform's to decide: a Docker network here (`WEIR_AGENT_DOCKER_ARGS`), a VPC or a security group in a cloud. A step's `shell.hosts` applies only to an agent run inline.
- Add the toolchains your code needs to the image; `deploy/agent.Dockerfile` says how.
- Skills and settings in the image's home directory (`~/.claude`) are loaded, because whoever built the image put them there. A repository's own are ignored. An agent run inline loads neither.

Know the limits:

- **Proven on a Mac with Docker Desktop.** Weir started a real container, and a real agent inside it edited a file, deleted one with the shell and wrote a third. It ran as the container's own user and saw none of Weir's variables. Docker on a Linux host has not been run.
- **The cost is the agent's word.** The container reports what the model call cost. Sending model calls through Weir, so that Weir counts them itself and the container holds no model credential, is not built.
- A directory that packs to more than 30 MB cannot be sent, and neither can a result larger than that.
- With `docker`, every `ai` step runs in a container. `decide` steps do not: they have no tools.
- Docker only. Cloud Run, Fargate and AgentCore are not built.

### An agent that works in a prepared directory

An agent that fixes code has to start inside the code. An `ai` step with `workspace:` names registered code that fills the agent's working directory before it runs. The agent changes the directory in place, and Weir keeps what it added, changed or deleted.

```yaml
- id: fix
  kind: ai
  prompt: fix-dependency@2
  input: { repo: input.repo, ref: input.ref }
  workspace:
    handler: github.checkout        # registered code; given the step's input and "with"
    with: { connection: github }
  shell: { hosts: [proxy.golang.org] }
  writes: { max_files: 50 }         # limits on what may change; the defaults apply if left out
  output: { schema: { type: object } }
  budget: { usd: 5 }
```

- The handler runs on the worker immediately before the agent, outside the agent's sandbox. Register one with `Registry.RegisterWorkspace`. Publishing refuses a name that is not registered.
- What changed is worked out by Weir: it records the directory before the agent starts and compares afterwards. It does not ask the agent, and it runs no program in the directory once the agent has been in it.
- The handler can name paths to leave out, in the form a `.gitignore` uses, for installed dependencies and build output. They are fixed before the agent starts.
- A `.git` directory is never read back, whatever the agent did to it.
- Changes are kept like any other files a step writes: `steps.fix.files` lists them, with `deleted: true` on a file that was removed, and the API lists and downloads them.
- The limits are on what changed, not on what the directory holds. Going over fails the step. A new link, or anything else that is not an ordinary file, also fails it.
- A step with a workspace cannot also list `files:`.
- `workspace: { handler: …, discard: true }` keeps nothing the agent changes. It is for a step that reads a directory and must leave no mark, such as an assessment.
- Not built: whether a file is executable is not recorded; the Codex adapter cannot do this.

One handler ships with Weir, `github.checkout`. It clones one repository into the agent's directory.

| Read from | Name | Meaning |
|---|---|---|
| the step's input | `repo` | The repository, as `owner/name`. Not needed if the connection is for one repository |
| the step's input | `ref` | A branch or tag. The default branch if left out |
| `with` | `connection` | The connection to use. `github` if left out |
| `with` | `depth` | How much history to fetch. 1 if left out |

- It asks for a read-only token for that one repository, from a GitHub App connection or a bearer token.
- The token is given to git for the one command, through its environment. It is not in the remote's address, the repository's configuration or a process listing, so an agent with a shell cannot find it afterwards.
- Git runs with no configuration but Weir's own, hooks off, and no submodules.
- Every `.gitignore` in the repository, at any depth, decides what is left out of the agent's changes.
- The worker needs `git` installed.

Proven against the real GitHub: a private repository was cloned as a GitHub App, and the token was nowhere in the directory.

### Committing what an agent changed

`github.commit` is a deterministic handler. It turns the files a step changed into a branch and a pull request, and it is the only thing that ever holds a token that can write.

```yaml
- id: fix
  kind: ai
  prompt: fix-dependency@2
  input: { repo: input.repo }
  workspace: { handler: github.checkout }
  output: { schema: { type: object } }
  budget: { usd: 5 }

- id: propose
  kind: deterministic
  handler: github.commit
  input:
    repo: input.repo
    from: '"fix"'                          # the step whose files to commit
    branch: '"weir/fix-" + run.id'
    title: '"Fix the vulnerable dependency"'
    body: steps.fix.output.summary
```

| Input | Meaning |
|---|---|
| `from` | The step whose files to commit |
| `branch` | The new branch to push |
| `title`, `body` | The pull request's title and description |
| `repo` | The repository, as `owner/name`. Not needed if the connection is for one repository |
| `base` | The branch to start from and merge into. The default branch if left out |
| `message` | The commit's message. The title if left out |
| `author` | `name` and `email` for the commit. `weir` if left out |
| `connection` | The connection to use. `github` if left out |

- It clones the repository afresh. Nothing from the directory the agent worked in is used, so nothing an agent left behind can run.
- The token that can write is asked for only at this point, after the agent has exited, for this one repository.
- It will not write to the base branch, into a `.git` directory, or through a link that leads out of the repository.
- Running it twice is safe: a branch that is already there is not pushed again, and a pull request that is already open is returned.
- If the files make no difference, it says so and opens nothing.
- Its output holds the branch, the commit and the pull request's number and address.

Proven against the real GitHub, end to end: a repository was checked out as a GitHub App, a real agent in a container of its own added one file, and this handler pushed a branch and opened a pull request. Whether a file is executable is not carried over.

### Paths an agent may not change

Weir checks every path a step's agent wrote, changed or deleted, after the agent has exited. One change that is not allowed fails the step, and nothing it changed is kept.

```yaml
- id: cover                       # may write tests and nothing else
  kind: ai
  prompt: add-tests@4
  workspace: { handler: github.checkout }
  writes:
    only: ["*_test.go", "testdata/"]
    never: ["billing/"]
```

- `only` lists the only paths that may change. `never` lists paths that may not. Both use the form a `.gitignore` uses, so `*_test.go` matches at any depth and `billing/` means everything under it.
- Some paths are protected whatever a step says, because changing them changes what runs later: `.git/`, `.github/workflows/`, `.claude/`, `CLAUDE.md` and `AGENTS.md`. A step that really must change them sets `writes: { protected: false }`.
- Protected wins over `never`, which wins over `only`.
- The agent is told the rules, as a courtesy. The check does not depend on it having listened.
- The rules apply to files written under `writes:` as well as changes to a prepared directory.

### A shell for an agent

An agent that changes code has to run it. An `ai` step with `shell:` gives its agent a shell, inside a sandbox. This is for agents run inline, beside the worker. An agent in a container of its own already has a shell and needs none of this.

```yaml
- id: fix
  kind: ai
  prompt: fix-dependency@2
  shell:
    hosts: [proxy.golang.org, sum.golang.org, "*.githubusercontent.com"]
  writes: {}
  output: { schema: { type: object } }
  budget: { usd: 5, minutes: 30 }
```

It is off until whoever runs Weir sets `WEIR_AGENT_SHELL=on`. With it on, the shell:

- can write only inside the step's working directory;
- can reach only the hosts the step lists, and with none listed has no network. A name beginning `*.` covers every host under it;
- cannot read `WEIR_SECRETS_DIR`, or the places in a home directory that hold credentials (`~/.ssh`, `~/.aws`, `~/.config/gcloud` and the like);
- sees the short list of environment variables an agent is given, not the worker's own;
- has no way to ask to leave the sandbox.

The sandbox is the Claude command line's own: Seatbelt on macOS, and `bwrap` with `socat` on Linux. A worker that cannot provide it fails the step and says why. It never runs a shell without one.

Know the limits before turning it on:

- **It is for one team running its own Weir.** The agent runs on the worker, which holds the database password and every workspace's secrets. The sandbox keeps the shell away from them; on a shared or hosted Weir that is not enough separation, and the agent should run somewhere isolated (not built).
- **Anything named in `WEIR_AGENT_ENV` is visible to the shell,** as is the agent's own sign-in. A step that lists a host can send them there. List only hosts you trust.
- **Proven on macOS only.** A real agent wrote inside its directory and reached a listed host, and was refused a write to the home directory, an unlisted host, the cloud metadata address and a secrets directory. Linux has not been run.
- The worker's image needs whatever the code needs: Go, Node, Python and so on.
- The Codex adapter cannot give an agent a shell.

### Schedules

A workflow can start by itself. Give a trigger a `schedule` in place of an `event`:

```yaml
triggers:
  - schedule: "0 7 * * 1-5"        # 07:00 UTC, Monday to Friday
    input: { due: event.data.at }   # optional; without it the run's input is {}
```

The schedule is a five-field cron expression: minute, hour, day of the month, month, day of the week. Each field is `*`, a number, a range (`1-5`), a list (`1,3,5`) or a step (`*/15`). Times are in UTC.

- A schedule that is due starts exactly one run, however many workers are looking.
- If Weir was not running when a schedule fell due, it fires once when Weir is back. It does not catch up on every time it missed.
- Publishing a workflow without the schedule stops it. `GET /v1/triggers` lists each schedule with the time it is next due.

Not yet: time zones other than UTC.

### Hooks, and checking they came from Weir

A workflow can tell an application when a run moves. Give the hook a shared secret and Weir signs it, so the receiver can check it came from Weir and is not an old one being replayed.

```yaml
hooks:
  - on: [run.*, step.waiting]
    connection: my-app
    path: /weir/hooks
    secret: env:HOOK_SECRET       # read from WEIR_SECRET__<WORKSPACE>__HOOK_SECRET
```

Each signed hook carries two more headers:

| Header | Value |
|---|---|
| `Weir-Timestamp` | When it was sent, in seconds since 1970 |
| `Weir-Signature` | `sha256=` and the HMAC-SHA256, as hex, of `<timestamp>.<body>` under the shared secret |

To check one: work out the same HMAC over the timestamp, a full stop and the exact bytes of the body; compare it with the header in constant time; and refuse the hook if the timestamp is more than a few minutes old. `Weir-Hook-Id` is the same on every attempt at one hook, so a receiver that has seen it can ignore a repeat.

If the secret is not set on the server, the hook is not sent unsigned. It waits and is tried again, like any failed delivery, and the run's record says which variable to set.

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
