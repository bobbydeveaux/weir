# connections Specification

## Purpose
A connection is a named address in a workspace that Weir calls on a workflow's behalf. It says how Weir proves who it is to the other side, by naming a secret that the platform holds and Weir never stores.

## Requirements

### Requirement: A connection is a named address in one workspace
A workspace SHALL make or replace a connection with `PUT /v1/connections/{name}`, giving its `base_url` and its `kind`, which is `http` (the default) or `mcp`. The connection SHALL belong to that workspace alone, and `GET /v1/connections` SHALL list only the workspace's own. Weir SHALL refuse a connection with no `base_url` or with any other kind. Making an `mcp` connection SHALL ask the server for its tools at once, and reply 502 if it cannot be asked.

#### Scenario: One workspace connects an MCP server
- **WHEN** the workspace `acme` makes an `mcp` connection called `kb` to a server that offers `search`
- **THEN** the reply lists the tool `kb.search`
- **AND** the workspace `rival` cannot see the tool or publish a workflow that uses it

#### Scenario: A workflow names a connection that does not exist
- **WHEN** a definition with a hook to the connection `app` is published before `app` is made
- **THEN** publishing is refused

### Requirement: A credential is named by reference and never stored
A connection's `auth` SHALL hold a kind and a reference to a secret, written `env:NAME` or `file:name`, and never the secret's value. The reply to making a connection SHALL say whether the secret can be found (`secret_found`) and where it is read from (`secret_read_from`). Weir SHALL read the value when a call is made, and MUST NOT write it to the database, an event, a recorded tool call or any reply.

#### Scenario: A connection names its secret
- **WHEN** the workspace `acme` makes a connection with `auth: {kind: bearer, secret: env:APP_TOKEN}` and the server has that secret
- **THEN** the reply holds `secret_found: true` and `secret_read_from: WEIR_SECRET__ACME__APP_TOKEN`
- **AND** the list of connections shows the kind `bearer` and the reference `env:APP_TOKEN`

#### Scenario: The value is looked for after runs have used it
- **WHEN** runs have made calls, fetched files, called tools and sent hooks through the connection
- **THEN** the secret's value is in no reply from the API and in no row Weir keeps

### Requirement: Malformed sign-in settings are refused when the connection is made
Weir SHALL reply 400 to a connection whose `auth` is not well formed: a kind that is not one of `none`, `bearer`, `header`, `basic`, `github-app` or `id-token`; a reference that is not `env:NAME` or `file:name`; a `header` kind with no header name, or one that takes over `Host`, `Content-Type`, `Content-Length` or `Idempotency-Key`; a `basic` kind with no user name or one with a colon in it; a `bearer`, `header` or `basic` kind with no secret; or a secret with no kind.

#### Scenario: A reference that is a path
- **WHEN** a connection is made with the secret `file:../rival/token`
- **THEN** the reply is 400

#### Scenario: A header kind with no header
- **WHEN** a connection is made with `auth: {kind: header, secret: env:APP_TOKEN}`
- **THEN** the reply is 400

### Requirement: Each kind of auth signs a request in its own way
Weir SHALL sign a request according to the connection's auth kind, by setting a header, and SHALL leave the request as it is for `none` or no auth at all. A `github-app` connection's secret MUST NOT be sent: a plain call through such a connection SHALL be refused and nothing added to the request.

#### Scenario: A bearer connection
- **WHEN** a call is made through a connection with `auth: {kind: bearer, secret: env:KEY}`
- **THEN** the request carries `Authorization: Bearer <secret>`

#### Scenario: A header connection
- **WHEN** a call is made through a connection with `auth: {kind: header, header: X-Api-Key, secret: env:KEY}`
- **THEN** the request carries `X-Api-Key` with the secret's value

#### Scenario: A basic connection
- **WHEN** a call is made through a connection with `auth: {kind: basic, username: weir, secret: env:KEY}`
- **THEN** the request carries `Authorization: Basic` and the base64 of `<username>:<secret>`

#### Scenario: An id-token connection
- **WHEN** a call is made through a connection of kind `id-token`
- **THEN** the request carries `Authorization: Bearer <token>`, where the token is the server's own identity

#### Scenario: A plain call through a GitHub App connection
- **WHEN** something other than the GitHub tools signs a request for a `github-app` connection
- **THEN** signing is refused and the request has no `Authorization` header

### Requirement: The credential is added to every call made through the connection
Weir SHALL sign every call it makes through a connection: an external step's call, a document fetched for an agent, an agent's tool call, a hook, and the calls to an MCP server. An agent SHALL name the connection and MUST NOT be given the credential.

#### Scenario: One connection, five kinds of call
- **WHEN** a workflow fetches a document, calls a tool, makes an external call and sends a hook through the connection `app`, and uses a tool from the MCP connection `kb`
- **THEN** every request the third party receives carries the connection's credential
- **AND** the result of the agent's tool call does not hold the secret

### Requirement: A missing secret says where to put it and sends nothing
When a connection's secret cannot be found, Weir MUST NOT make the call. The error SHALL name the environment variable or file the secret is read from, and SHALL NOT hold a value. An external step that cannot be signed SHALL fail with the error type `secret_unavailable`.

#### Scenario: A step whose secret is not set
- **WHEN** a run in the workspace `rival` reaches an external step through a connection whose secret `env:APP_TOKEN` is not set
- **THEN** the run is `blocked`, with the error type `secret_unavailable` and a message naming `WEIR_SECRET__RIVAL__APP_TOKEN`
- **AND** the third party receives no call

### Requirement: A credential is not followed to another host
When a third party answers a signed call with a redirect to a different host, Weir SHALL NOT follow it. The caller SHALL get the redirect itself as the reply.

#### Scenario: A redirect to somewhere else
- **WHEN** a call carrying a credential is answered with a redirect to another host
- **THEN** the other host receives no request and no credential

### Requirement: A workspace reads only its own secrets
A reference SHALL be a short name, not a variable or a path. `env:NAME` in the workspace `acme` SHALL be read from the variable `WEIR_SECRET__ACME__NAME` and from nowhere else, so a workspace cannot name the server's own settings or another workspace's secret. No two pairs of workspace and name SHALL be read from the same variable.

#### Scenario: A workspace names the server's own setting
- **WHEN** the workspace `acme` names the secret `env:WEIR_ADMIN_KEY`
- **THEN** it resolves to nothing, although the server has a variable of that name

#### Scenario: Two workspaces name the same secret
- **WHEN** `acme` and `rival` each name `env:GITHUB_TOKEN`
- **THEN** each gets its own value and neither gets the other's

### Requirement: File secrets stay inside the workspace's directory
`file:name` in the workspace `acme` SHALL be read from `$WEIR_SECRETS_DIR/acme/name`, with a trailing newline dropped. The name MUST be a plain file name: Weir SHALL refuse one with a directory, a `..`, or a leading dot. With `WEIR_SECRETS_DIR` not set, a `file:` reference SHALL fail with an error that names the setting.

#### Scenario: A name that climbs out
- **WHEN** the workspace `acme` names `file:../rival/token`
- **THEN** it is refused and no file is read

#### Scenario: No directory is set
- **WHEN** a `file:` secret is asked for on a server with no `WEIR_SECRETS_DIR`
- **THEN** the error names `WEIR_SECRETS_DIR`

### Requirement: A connection can sign in as the server itself
A connection of kind `id-token` SHALL take no secret; naming one is refused with 400. For each call Weir SHALL ask the platform for an ID token made for the address being called (its scheme and host) and send it as a bearer token. This is off unless the server is started with `WEIR_ID_TOKENS=google`: with it off, the call SHALL NOT be made and the error SHALL name the setting. It works on Google Cloud only.

#### Scenario: The setting is off
- **WHEN** a call is made through an `id-token` connection on a server without `WEIR_ID_TOKENS`
- **THEN** nothing is sent and the error names `WEIR_ID_TOKENS`

#### Scenario: The setting is on
- **WHEN** a call is made to `https://app.example.com/hooks/weir` on a server with `WEIR_ID_TOKENS=google`
- **THEN** the platform is asked for a token for `https://app.example.com`
- **AND** the request carries that token as `Authorization: Bearer`
