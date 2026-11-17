# agent-demo on Weir

The first application integrated against Weir. agent-demo (a separate, private demonstration application) onboards a reinsurance treaty:
a document goes in and a configured data room comes out. It used to carry its own six-step engine. With `WEIR_URL` set,
the workflow runs in Weir instead, and agent-demo talks to Weir only over its HTTP API.

Proven locally on 16 November 2026 with the Ashbourne sample treaty: upload, extraction, validation, approval by a
second person, data room created as that person, brief written and stored. Two model calls, 15 tool calls, $0.60
(on the command line's default model; the prompts' own model is now passed through, which is cheaper).

## Who does what

| | agent-demo | Weir |
|---|---|---|
| The uploaded file, the data rooms, the knowledge base | Owns them | Never stores them |
| The workflow definition and the two prompts | Writes them and publishes them at start | Stores them as immutable versions |
| Order of steps, retries, waiting, budget | | Owns it |
| Running the agents | | Runs them, locked down |
| The agents' tools | Serves them (two MCP servers) | Offers them to agents through its tool proxy; checks and records every call |
| Validation, creating the room, storing the brief | Its own code, behind three routes | Calls those routes as `external` steps |
| Starting a run | Reports `treaty.uploaded`; never names a workflow | Matches the event to the workflow's `triggers:` and starts the run |
| The approval | Checks the rules, then records the decision in Weir | Holds the human task; refuses a second decision |
| Knowing a run has moved | Listens on `/weir/hooks`; never asks | Posts a hook each time the run or a step changes, and tries again if it is refused |
| What the dashboard shows | The list comes from what the hooks reported; one workflow's page is read from Weir's API | Serves runs, steps, model calls, tool calls, cost |

## The calls

```
agent-demo → Weir                                   Weir → agent-demo
POST /v1/workspaces            (once)               GET  /weir/files/{id}     the treaty, for the agent
PUT  /v1/connections/agent-demo                     POST /knowledge/mcp       the agent's tools
PUT  /v1/connections/knowledge  (kind: mcp)
PUT  /v1/connections/datarooms  (kind: mcp)
POST /v1/templates/{key}       (when a prompt changes)  POST /tools/mcp
POST /v1/definitions           (when the workflow changes)  POST /weir/validate
POST /v1/events                on upload: "treaty.uploaded"   POST /weir/datarooms
GET  /v1/runs/{id}, /record    for the dashboard    POST /weir/finish
GET  /v1/tasks
POST /v1/tasks/{id}/decision   approve or reject
POST /v1/runs/{id}/resume      retry a failed step     POST /weir/hooks          a run or a step moved
```

Every call from agent-demo carries `X-Weir-Workspace: agent-demo`, so its workflows, runs and usage are kept apart from any other tenant's.

## Run it

```bash
# 1. Weir
docker compose up -d && make build                      # in this repository
export WEIR_DATABASE_URL='postgres://weir:weir@localhost:54329/weir?sslmode=disable'
./bin/weir migrate
WEIR_HTTP_ADDR=:8080 ./bin/weir serve

# 2. agent-demo, pointed at it
WEIR_URL=http://localhost:8080 ./dev.sh                  # in agent-demo

# 3. The console
cd console && WEIR_API_URL=http://localhost:8080 npm run dev
```

Either can start first: each waits for the other. Without `WEIR_URL`, agent-demo runs its own engine as before.

## How a change reaches the screen

```
Weir commits an event  ->  hook to agent-demo (/weir/hooks)  ->  agent-demo reads the run back and keeps it
                                                              ->  agent-demo tells its browsers (/api/changes)
                                                              ->  the page watching that workflow reloads
```

A browser never talks to Weir. Weir has streams of its own (`/v1/runs/{id}/stream`, `/v1/stream`), which its console uses;
an application's users are the application's to tell, in the application's own terms and behind its own sign-in.

## What Weir had to learn for this

- **Events and triggers.** `POST /v1/events` and a `triggers:` block in the workflow. A second workflow can listen for the same event without agent-demo changing.
- **Hooks.** A `hooks:` block in the workflow names what to report and where. Hooks are queued in the same transaction as the thing they report, and retried with a growing wait.
- **Workspaces on every request.** `X-Weir-Workspace` scopes every read and write; one worker serves every workspace.
- **A read side.** Workflows, versions, runs, the audit record, templates, connections, usage.
- **Documents for agents.** An `ai` step lists `files`; Weir fetches them and the agent may read those and nothing else.
- **MCP connections.** A connection of kind `mcp`, made over the API, brings that server's tools into one workspace and no other. They are still reached only through the tool proxy. agent-demo connects its own two servers at start.
- **Data on a decision.** A person can send values with an approval (here, the edited terms); later steps read them.
- **The prompt and the reply are kept** with each model call.
- **A template can name its model.**

## What is rough

- A failed `external` step shows only the status code, not the reason agent-demo gave.
- The three `/weir/*` routes and Weir's API are unauthenticated; both sides trust the local network. API keys are the next security epic.
- An MCP connection's tools are read when the connection is made. If the server later adds a tool, the connection must be put again.
- A workspace can point a connection at any address Weir can reach. A hosted service needs a rule for which addresses are allowed.
- Nothing polls on a short timer any more. agent-demo passes each hook on to its own browsers as a stream (`GET /api/changes`), and a page reloads when told its workflow changed. A 15-second look remains as a safety net, and one workflow's page still asks Weir twice each time it reloads.
- A hook says something moved, not what the new state is; agent-demo reads the state back. That is deliberate (hooks can arrive late, twice or out of order) but it means two calls per hook.
