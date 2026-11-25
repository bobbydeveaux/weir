# access Specification

## Purpose
Access decides who may call the API and what they may do: an admin key, API keys with roles that each belong to one workspace, and a sign-in proxy that vouches for a person. It also says who is recorded as the actor of a request.

## Requirements

### Requirement: Without an admin key the API trusts its callers
A server started without `WEIR_ADMIN_KEY` and without a sign-in proxy SHALL answer any caller, and SHALL take the workspace and the actor from the request. The actor is the value of the `X-Weir-Actor` header. A request that publishes, starts a run, decides, resumes, cancels, reports an event, or creates a key or a workspace MUST name an actor, and is answered 400 when it does not.

#### Scenario: A write with no actor
- **WHEN** a workflow is published to an open server with no `X-Weir-Actor` header
- **THEN** the reply is 400, and says to set the header

#### Scenario: A write with an actor
- **WHEN** `alice` starts a run on an open server with `X-Weir-Actor: alice`
- **THEN** the run is started, and `alice` is its requester

### Requirement: With an admin key set every request needs a key
When `WEIR_ADMIN_KEY` is set, a request MUST carry a key as `Authorization: Bearer <key>`. A request with no key, an unknown key or a revoked key SHALL be answered 401 with a `WWW-Authenticate: Bearer` header.

#### Scenario: No key
- **WHEN** `GET /v1/definitions` is sent with no `Authorization` header to a server with an admin key
- **THEN** the reply is 401

#### Scenario: A made-up key
- **WHEN** the same request carries the key `weir_made_up`
- **THEN** the reply is 401

### Requirement: A key is shown once and only its hash is stored
`POST /v1/keys` with a `name`, a `role` and an optional `act_as` SHALL create a key for the request's workspace and answer 201 with the key's secret, which begins `weir_`. The secret MUST NOT be stored: Weir keeps only a hash of it, and no later reply shows it. `GET /v1/keys` SHALL list the workspace's keys without their secrets.

#### Scenario: A key is created
- **WHEN** the admin key posts `{"name": "the-app", "role": "writer"}` to `/v1/keys` for the workspace `acme`
- **THEN** the reply is 201 with a secret that begins `weir_`, and a key whose workspace is `acme`

#### Scenario: Keys are listed
- **WHEN** an admin of a workspace calls `GET /v1/keys`
- **THEN** the list holds that workspace's keys only, and none has a secret

### Requirement: A key belongs to one workspace
A key SHALL work in the workspace it was made for and no other. A request with such a key needs no `X-Weir-Workspace` header. A request that names a different workspace MUST be answered 403.

#### Scenario: A key with no workspace header
- **WHEN** a key that belongs to `acme` publishes a workflow with no `X-Weir-Workspace` header
- **THEN** the workflow is published in `acme`

#### Scenario: A key names another workspace
- **WHEN** the same key calls `GET /v1/definitions` with `X-Weir-Workspace: rival`
- **THEN** the reply is 403

### Requirement: A role decides what a key may do
A key's role MUST be `reader`, `writer` or `admin`, and any other role is refused when the key is made. A `reader` SHALL only read. A `writer` SHALL also publish, start, decide, resume and cancel. An `admin` SHALL also list, create and revoke its workspace's keys. The admin key SHALL do anything in any workspace, and only the admin key or a person signed in through a proxy SHALL create a workspace. A request a role does not allow is answered 403.

#### Scenario: A reader tries to start a run
- **WHEN** a key with the role `reader` calls `POST /v1/runs`
- **THEN** the reply is 403

#### Scenario: A writer tries to list keys
- **WHEN** a key with the role `writer` calls `GET /v1/keys`
- **THEN** the reply is 403

#### Scenario: A workspace's key tries to create a workspace
- **WHEN** a key that belongs to `acme` calls `POST /v1/workspaces`
- **THEN** the reply is 403

### Requirement: A key acts as itself unless it may act for others
With keys on, the actor of a request SHALL be the key's own name, whatever the `X-Weir-Actor` header says. A key made with `act_as` SHALL instead be recorded as the person the header names, when it names one. The admin key is recorded as `admin`, and may also name the person it acts for.

#### Scenario: An application names its user
- **WHEN** a key made with `act_as` starts a run with `X-Weir-Actor: alice@example.com`
- **THEN** the run's requester is `alice@example.com`

#### Scenario: A plain key names someone else
- **WHEN** a key named `ci`, made without `act_as`, starts a run with `X-Weir-Actor: ceo@example.com`
- **THEN** the run's requester is `ci`

### Requirement: A revoked key stops working at once
`DELETE /v1/keys/{id}` SHALL revoke one of the workspace's keys. The next request with that key MUST be answered 401.

#### Scenario: A key is revoked
- **WHEN** an admin revokes a key, and the key is then used for `GET /v1/runs`
- **THEN** the reply is 401

### Requirement: Some paths need no key
With keys on, these paths SHALL still answer without one: `/healthz`, and anything under `/v1/callbacks/`, `/v1/tools/`, `/v1/agent/` and `/v1/webhooks/`. A callback, a tool call and an agent's requests carry a one-time token for a single attempt, and a webhook carries a signature. Those are their credentials.

#### Scenario: The health check
- **WHEN** `GET /healthz` is sent with no key to a server with an admin key
- **THEN** the reply is 200

#### Scenario: An agent in its own container
- **WHEN** an agent fetches its job from `/v1/agent/{token}/job` with its step's token and no key
- **THEN** the job is returned
- **AND** `GET /v1/runs/{id}/files` with no key is still answered 401

### Requirement: A sign-in proxy can vouch for a person
When `WEIR_PROXY_USER_HEADER` names a header, a request that carries that header SHALL need no key. The person it names may do anything in any workspace, and is the actor on what they do. Keys SHALL go on working beside it. Weir does not check a signature on the header, and a server that is not given the setting MUST ignore it.

#### Scenario: A signed-in person
- **WHEN** a request carries the proxy's header with `accounts.google.com:lead@example.com`, and the proxy's own token in `Authorization`
- **THEN** the person is `lead@example.com`: anything before the last colon is dropped
- **AND** the `Authorization` header is not read as a key
- **AND** the person can create a workspace

#### Scenario: Who the server takes a caller to be
- **WHEN** `GET /v1/me` is requested by a signed-in person, and then with the admin key
- **THEN** each reply holds a name, a role and a workspace
- **AND** `signed_in` is true for the person and false for the key

#### Scenario: Neither a key nor the header
- **WHEN** a request to the same server carries no key and no proxy header
- **THEN** the reply is 401

#### Scenario: A server that trusts no proxy
- **WHEN** the header is sent to a server that was not given `WEIR_PROXY_USER_HEADER`
- **THEN** the reply is 401

### Requirement: A server can be made to refuse to start open
When `WEIR_REQUIRE_ADMIN_KEY` is set and neither `WEIR_ADMIN_KEY` nor `WEIR_ADMIN_KEY_FILE` is, `weir serve` MUST refuse to start and say why. The server's container image sets `WEIR_REQUIRE_ADMIN_KEY`. Without any of these settings the server SHALL start, and SHALL print a warning that anyone who can reach it can do anything.

#### Scenario: The image is started with no admin key
- **WHEN** `weir serve` is started with `WEIR_REQUIRE_ADMIN_KEY=1` and no `WEIR_ADMIN_KEY` or `WEIR_ADMIN_KEY_FILE`
- **THEN** it exits with an error that names all three settings

#### Scenario: The key comes from a file
- **WHEN** `weir serve` is started with `WEIR_REQUIRE_ADMIN_KEY=1` and `WEIR_ADMIN_KEY_FILE` naming a file that holds a key
- **THEN** the requirement is met, and the server goes on to start

#### Scenario: A laptop with no admin key
- **WHEN** `weir serve` is started with none of these settings
- **THEN** it starts, and prints a warning that no admin key is set

### Requirement: The admin key can be read from a file
`WEIR_ADMIN_KEY_FILE` SHALL name a file holding the admin key, read once when `weir serve` starts. The key is the file's contents with surrounding whitespace trimmed, and it SHALL act as `WEIR_ADMIN_KEY` does. `weir serve` MUST refuse to start when both settings are set, when the file cannot be read, or when it holds only whitespace. Each error SHALL name the setting to change, and MUST NOT contain the key.

#### Scenario: The key is in a file
- **WHEN** `weir serve` is started with `WEIR_ADMIN_KEY_FILE` naming a file that holds the key followed by a newline
- **THEN** the admin key is the key without the newline

#### Scenario: Both settings are given
- **WHEN** `weir serve` is started with both `WEIR_ADMIN_KEY` and `WEIR_ADMIN_KEY_FILE` set
- **THEN** it exits with an error that names both settings, and does not contain either key

#### Scenario: The file is missing
- **WHEN** `weir serve` is started with `WEIR_ADMIN_KEY_FILE` naming a file that does not exist
- **THEN** it exits with an error that names `WEIR_ADMIN_KEY_FILE`

#### Scenario: The file is empty
- **WHEN** `weir serve` is started with `WEIR_ADMIN_KEY_FILE` naming a file that holds only a newline
- **THEN** it exits with an error that names `WEIR_ADMIN_KEY_FILE` and says the file is empty
