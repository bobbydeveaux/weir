# workspaces Specification

## Purpose
A workspace is one tenant: a team, a customer or an application. Everything Weir holds belongs to one workspace, and a workspace cannot see or reach anything that belongs to another.

## Requirements

### Requirement: Every database has a default workspace
Migrating a database SHALL create one workspace with the slug `default`. A request that names no workspace SHALL be for `default`.

#### Scenario: A new database is migrated
- **WHEN** an empty database is migrated
- **THEN** it holds one workspace, and its slug is `default`

#### Scenario: A request names no workspace
- **WHEN** a request arrives with no `X-Weir-Workspace` header and no `workspace` parameter
- **THEN** it reads and changes the `default` workspace only

### Requirement: A request names its workspace
A request SHALL name its workspace by slug in the `X-Weir-Workspace` header. Where a header cannot be set, the `workspace` query parameter SHALL do the same, and the header wins when both are present. A request that names a workspace that does not exist MUST be answered 404.

#### Scenario: An unknown workspace
- **WHEN** a request is made with `X-Weir-Workspace: nobody`, and no such workspace exists
- **THEN** the reply is 404

#### Scenario: The workspace is named in the address
- **WHEN** a stream is opened at `/v1/stream?workspace=acme`
- **THEN** it reports the runs of `acme`

### Requirement: A workspace is created over the API
`POST /v1/workspaces` with a `slug` and an optional `name` SHALL create a workspace and answer 201 with the slug. A slug MUST start with a lower-case letter and hold only lower-case letters, digits and hyphens. A name that is left out SHALL be the slug. Creating a workspace that already exists SHALL change nothing.

#### Scenario: A workspace is created
- **WHEN** `POST /v1/workspaces` is sent with `{"slug": "acme", "name": "Acme"}`
- **THEN** the reply is 201
- **AND** requests that name `acme` are answered

#### Scenario: A slug that is not allowed
- **WHEN** `POST /v1/workspaces` is sent with the slug `Acme Ltd`
- **THEN** the reply is 400 and no workspace is created

### Requirement: Workspaces can be listed
`GET /v1/workspaces` SHALL list the workspaces in order of slug, each with its slug, its name, when it was created, and how many workflows and runs it holds. A key that belongs to one workspace SHALL be shown that workspace and no other.

#### Scenario: A workspace's key lists workspaces
- **WHEN** a key that belongs to `acme` calls `GET /v1/workspaces`, and `rival` also exists
- **THEN** the list holds `acme` only

### Requirement: Definitions, templates, runs and tasks are kept apart
A workspace SHALL see only its own definitions, templates, runs, tasks and usage. Another workspace's lists MUST come back empty and its single items MUST be answered 404. A workspace MUST NOT publish a workflow that uses another's template, start another's workflow, or decide another's task.

#### Scenario: A second workspace looks
- **WHEN** `acme` has a workflow, a run and an open task, and the `default` workspace lists definitions, runs and tasks
- **THEN** all three lists are empty
- **AND** reading the run, its record or the definition by name from `default` is answered 404

#### Scenario: A second workspace tries to act
- **WHEN** the `default` workspace decides a task that belongs to `acme`
- **THEN** the reply is 404 and the task is still open

### Requirement: Events and schedules reach only their own workspace
An event reported to a workspace, a webhook received for it, and a schedule that falls due in it SHALL start runs in that workspace only, even when another workspace has a trigger for the same event.

#### Scenario: Two workspaces want the same event
- **WHEN** `default` and `rival` each have a workflow triggered by `file.uploaded`, and the event is reported to `default`
- **THEN** runs start in `default`
- **AND** `rival` has no runs

### Requirement: Connections, tools and secrets belong to one workspace
A connection SHALL belong to the workspace it was made in. The tools of an MCP connection SHALL be visible to, and usable by, that workspace only. A secret reference SHALL be read from that workspace's own secrets: a workspace MUST NOT be able to name another workspace's secret or one of the server's own settings.

#### Scenario: Another workspace names an MCP tool
- **WHEN** `acme` connects an MCP server that offers `kb.search`, and `rival` publishes a workflow that lists `kb.search`
- **THEN** publishing is refused
- **AND** `GET /v1/registry` for `rival` does not list `kb.search`

#### Scenario: Another workspace names the same secret
- **WHEN** `acme` has the secret `APP_TOKEN`, and `rival` makes a connection that names `env:APP_TOKEN`
- **THEN** the reply says the secret is not found for `rival`
- **AND** a step in `rival` that uses the connection makes no call

### Requirement: A run's files and streams are kept apart
A workspace SHALL list and download only the files of its own runs, and SHALL follow only its own runs as a stream. Another workspace's requests MUST be answered 404, and its workspace stream MUST stay silent.

#### Scenario: Another workspace asks for a file
- **WHEN** `rival` lists or downloads the files of a run that belongs to another workspace
- **THEN** the reply is 404

#### Scenario: Another workspace follows a run
- **WHEN** the `default` workspace opens the stream of a run that belongs to `acme`
- **THEN** the reply is 404
- **AND** the `default` workspace's own stream reports nothing when that run moves
