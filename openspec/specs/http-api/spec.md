# http-api Specification

## Purpose
The HTTP API is what an application integrates against. This says which endpoints exist, what each answers, and which status code each common refusal gets. The rules behind each feature are in that feature's own specification.

## Requirements

### Requirement: The API says it is up
`GET /healthz` SHALL answer 200 with `{"status": "ok"}`.

#### Scenario: A health check
- **WHEN** `GET /healthz` is requested
- **THEN** the reply is 200

### Requirement: Definitions are published and read
`POST /v1/definitions` SHALL take a workflow's YAML as its body and answer 201 with the `namespace`, `name` and `version` published, and a `not_yet_enforced` list of the parts that were accepted but are not enforced. `GET /v1/definitions` SHALL list the workspace's workflows. `GET /v1/definitions/{namespace}/{name}` SHALL return one, with its source as published, and `?version=` chooses a version.

#### Scenario: A workflow is published
- **WHEN** a valid workflow is posted to `/v1/definitions` for the first time
- **THEN** the reply is 201 with `version` 1

#### Scenario: A definition is read
- **WHEN** `GET /v1/definitions/support/triage` is requested in the workspace that holds it
- **THEN** the reply is 200 and holds the YAML source

### Requirement: Templates are published and read
`POST /v1/templates/{key}` SHALL publish a new version of a template and answer 201 with the `key` and its `version`. A template with no `user` prompt MUST be answered 400. `GET /v1/templates` SHALL list the workspace's templates, and `GET /v1/templates/{key}` SHALL return one template's versions.

#### Scenario: A template is published
- **WHEN** `{"user": "{{input}}"}` is posted to `/v1/templates/classify`
- **THEN** the reply is 201

#### Scenario: A template with no prompt
- **WHEN** `{}` is posted to `/v1/templates/classify`
- **THEN** the reply is 400

### Requirement: A run is started
`POST /v1/runs` SHALL take a `namespace`, a `name`, and optionally an `alias`, an `input` and an `idempotency_key`. It SHALL answer 201 with the run's `id` and `created` true. When a run was already started with the same idempotency key, it SHALL answer 200 with that run's `id` and `created` false. The requester is the request's actor.

#### Scenario: A run is started
- **WHEN** `{"namespace": "shop", "name": "refund", "input": {"order": "A-17"}}` is posted to `/v1/runs`
- **THEN** the reply is 201 with the new run's `id`

#### Scenario: The same key again
- **WHEN** the same request is sent twice with the idempotency key `order-42`
- **THEN** the second reply is 200, with the first run's `id` and `created` false

### Requirement: Runs are read, listed and counted
`GET /v1/runs/{id}` SHALL return a run with its status, steps and events. `GET /v1/runs/{id}/record` SHALL return its record, which holds the model calls, tool calls and hooks. `GET /v1/runs` SHALL list runs newest first. `GET /v1/usage` SHALL return totals of runs and spend, by day, by workflow and by model.

#### Scenario: A run is read
- **WHEN** `GET /v1/runs/{id}` is requested for a run that has finished
- **THEN** the reply is 200 and its `status` is `succeeded`

#### Scenario: Runs are filtered
- **WHEN** `GET /v1/runs` is requested with any of `namespace`, `name`, `status` and `requester`
- **THEN** only the runs that match are listed

#### Scenario: How many runs are listed
- **WHEN** `GET /v1/runs` is requested with no `limit`, and then with `limit=500`
- **THEN** the first reply holds at most 50 runs and the second at most 200

#### Scenario: Usage is narrowed
- **WHEN** `GET /v1/usage?days=7&namespace=support` is requested
- **THEN** the totals cover the last seven days of workflows in `support`

#### Scenario: A run that does not exist
- **WHEN** `GET /v1/runs/00000000-0000-0000-0000-000000000000` is requested
- **THEN** the reply is 404

### Requirement: A run is resumed or cancelled
`POST /v1/runs/{id}/resume` SHALL restart a blocked run and answer 200 with `{"status": "resumed"}`. A `budget_usd` above zero in the body replaces the run's budget. Resuming a run that is not blocked MUST be answered 409. `POST /v1/runs/{id}/cancel` SHALL cancel a run and answer 200 with `{"status": "cancelled"}`, and takes an optional `reason`. Cancelling a run that has already ended MUST be answered 403.

#### Scenario: A blocked run is cancelled
- **WHEN** `{"reason": "cannot be put right"}` is posted to a blocked run's `/cancel`
- **THEN** the reply is 200 and the run's status is `cancelled`

#### Scenario: A run is cancelled twice
- **WHEN** the same run is cancelled again
- **THEN** the reply is 403

### Requirement: Tasks are listed and decided
`GET /v1/tasks` SHALL list the workspace's open tasks, and answers an empty list when there are none. `POST /v1/tasks/{id}/decision` SHALL take an `outcome`, an optional `note` and optional `data`, and answer 200 with `{"status": "decided"}`. A decision the person is not allowed to make MUST be answered 403. A decision on a task that is no longer open MUST be answered 409.

#### Scenario: A task is decided
- **WHEN** `bob` posts `{"outcome": "approved"}` to an open task's `/decision`
- **THEN** the reply is 200

#### Scenario: A decision that is not allowed
- **WHEN** the run's requester posts a decision to a task that excludes the requester
- **THEN** the reply is 403

### Requirement: Events are reported and listed
`POST /v1/events` SHALL take an event's `type`, and optionally an `id`, a `source` and `data`. It SHALL answer 202 with the event's `id`, whether it was a `duplicate`, and the `runs` it started. `GET /v1/events` SHALL list the events the workspace has received, newest first: 50 of them, or the number `limit` gives when it is between 1 and 200. `GET /v1/triggers` SHALL list which events and schedules start which workflows.

#### Scenario: An event starts runs
- **WHEN** an event is posted that two workflows' triggers match
- **THEN** the reply is 202 and `runs` names both, each with a `run_id`

#### Scenario: An event nothing wants
- **WHEN** an event is posted that no trigger matches
- **THEN** the reply is 202 and `runs` is empty

### Requirement: Connections are made, listed and checked
`PUT /v1/connections/{name}` SHALL create or update a connection and answer 200. `GET /v1/connections` SHALL list the workspace's connections. `POST /v1/connections/{name}/check` SHALL answer 200 and say whether Weir can sign in. `GET /v1/registry` SHALL list the handlers, tools and decide providers the workspace can use.

#### Scenario: A connection is made
- **WHEN** a body with a `base_url`, and optionally a `kind` of `http` or `mcp`, an `auth` and a `webhook`, is put to `/v1/connections/payments`
- **THEN** the reply is 200

#### Scenario: A connection that cannot be accepted
- **WHEN** a connection is put with no `base_url`, or with a `kind` that is neither `http` nor `mcp`
- **THEN** the reply is 400

#### Scenario: An MCP server that is not there
- **WHEN** a connection of kind `mcp` is put with an address that cannot be asked for its tools
- **THEN** the reply is 502

### Requirement: A callback completes a waiting step
`POST /v1/callbacks/{token}` SHALL complete the attempt the token belongs to, with the body as its output, and answer 200 with `{"status": "accepted"}`. A token that was already used, or that never existed, MUST be answered 409. A body that is not JSON MUST be answered 400. The token is the credential: the request needs no key and names no workspace.

#### Scenario: A third party calls back
- **WHEN** `{"refunded": true}` is posted to the callback address a step gave out
- **THEN** the reply is 200

#### Scenario: The same callback again
- **WHEN** the same address is posted to a second time
- **THEN** the reply is 409

### Requirement: A run's files are listed and downloaded
`GET /v1/runs/{id}/files` SHALL list the files the run's steps produced. `GET /v1/runs/{id}/files/{step}/{path}` SHALL send one file as an attachment, with `X-Content-Type-Options: nosniff` and the file's digest in `Weir-File-Sha256`. A file that was never written, or was deleted, MUST be answered 404.

#### Scenario: A file is downloaded
- **WHEN** a file a step wrote is requested by its step and path
- **THEN** the reply is 200, its `Content-Disposition` is an attachment, and `Weir-File-Sha256` is set

#### Scenario: A file that was never written
- **WHEN** a path no step wrote is requested
- **THEN** the reply is 404

### Requirement: Runs can be followed as streams
`GET /v1/runs/{id}/stream` SHALL answer in the Server-Sent Events format with the run's events, first those that have already happened and then each new one as it happens. Each event's id is its sequence number, and a client that sends `Last-Event-ID` or `?after=` SHALL be sent only the events after it. `GET /v1/stream` SHALL send a message naming the `run_id` each time a run in the workspace moves. A stream of a run that is not in the workspace MUST be answered 404.

#### Scenario: A run is followed
- **WHEN** a client opens the stream of a run that has two events, and a person then decides its task
- **THEN** the client is sent events 1 and 2, and then event 3 without asking again

#### Scenario: A client reconnects
- **WHEN** a client opens the stream with `Last-Event-ID: 3`
- **THEN** the first event it is sent is event 4

### Requirement: A refusal has a status code and a reason
Every refusal SHALL be JSON of the form `{"error": "<a sentence>"}`, with a status code for its kind: 404 for something that does not exist in the workspace, 403 for something the caller may not do, 409 for something that is no longer open, 401 for a missing or bad key, and 400 for any other request Weir cannot accept.

#### Scenario: Something that is not there
- **WHEN** a run that does not exist is requested
- **THEN** the reply is 404 with an `error`

#### Scenario: Something that is no longer open
- **WHEN** a request uses a token that is not live, decides a task that is closed, or resumes a run that is not blocked
- **THEN** the reply is 409

#### Scenario: A request Weir cannot accept
- **WHEN** a workflow fails its checks, a body is not valid JSON, or a write names no actor on a server without keys
- **THEN** the reply is 400

#### Scenario: A body that is too large
- **WHEN** a request's body is over 1 MB
- **THEN** the reply is 413

#### Scenario: A tool that failed
- **WHEN** an agent calls a tool it is allowed, and the tool fails
- **THEN** the reply is 422

### Requirement: The API can also answer under a prefix
When `WEIR_API_PREFIX` names a path, the API SHALL answer under that path as well as at the root, so that `/api/v1/…` and `/api/healthz` work beside `/v1/…` and `/healthz`.

#### Scenario: A proxy that passes on one path
- **WHEN** the server is started with the prefix `/api`, and `GET /api/v1/workspaces` is requested
- **THEN** the reply is the same as for `GET /v1/workspaces`
