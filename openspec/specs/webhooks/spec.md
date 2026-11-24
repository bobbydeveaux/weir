# webhooks Specification

## Purpose
A connection can receive as well as call. A third party posts to an address on Weir, Weir checks the signature to be sure who sent it, and the request becomes an event that a trigger can start a workflow on.

## Requirements

### Requirement: A connection receives webhooks at an address of its own
A connection MAY be given a `webhook` setting: a `kind`, which is `github`, `hmac` or `none`, and a reference to the shared secret. The connection then SHALL receive at `POST /v1/webhooks/<workspace>/<connection>`, and the reply to making it SHALL give that path. The kind `none` SHALL switch receiving off. Weir SHALL reply 400 to a setting that is not well formed.

#### Scenario: A connection is told to receive
- **WHEN** the workspace `rival` makes the connection `github` with `webhook: {kind: github, secret: env:GH_WEBHOOK}`
- **THEN** the reply gives the path `/v1/webhooks/rival/github`
- **AND** it says whether the secret can be found and where it is read from

#### Scenario: A setting that is not well formed
- **WHEN** a webhook setting has an unknown kind, no secret, a `github` kind that names headers, or an `hmac` kind without its signature header or without a way to name the event
- **THEN** the reply is 400

#### Scenario: Receiving is switched off
- **WHEN** the connection is made again with `webhook: {kind: none}`, and a correctly signed request arrives
- **THEN** the reply is 404

### Requirement: The signature is the credential
The sender cannot hold an API key or send Weir's headers, so the workspace and the connection are in the address. Weir SHALL accept a request at that address with no API key, on a server that demands a key everywhere else, when and only when its signature is the HMAC-SHA256 of the exact body under the connection's shared secret. The signature MUST be checked before anything in the body is used.

#### Scenario: A signed request on a locked server
- **WHEN** the server has an admin key set, and a correctly signed request arrives with no API key
- **THEN** the reply is 202

### Requirement: GitHub's signature is checked as GitHub sends it
For the kind `github`, Weir SHALL read the signature from `X-Hub-Signature-256`, as `sha256=` followed by hex, the name of what happened from `X-GitHub-Event`, and the delivery id from `X-GitHub-Delivery`.

#### Scenario: A pull request webhook
- **WHEN** GitHub's headers arrive with a correct `X-Hub-Signature-256` and `X-GitHub-Event: pull_request`
- **THEN** the request is accepted as the event `github.pull_request` on the connection `github`

#### Scenario: The wrong scheme
- **WHEN** the header holds `sha1=` and a digest
- **THEN** the reply is 401

### Requirement: A general kind checks any sender that signs with HMAC-SHA256
For the kind `hmac`, the connection SHALL name the header that carries the signature as hex (`header`), and MAY name text that comes before the hex (`prefix`). It MUST say what happened, with either the header that names it (`event_header`) or one fixed name (`event`), and never both. It MAY name the header that carries the sender's delivery id (`id_header`).

#### Scenario: A sender with its own headers
- **WHEN** the connection `billing` names `X-Signature`, `X-Event` and `X-Delivery`, and a signed request arrives with `X-Event: invoice.paid`
- **THEN** the reply is 202 and the newest event has the type `billing.invoice.paid`

### Requirement: A refused request records nothing
Weir SHALL reply 401 to a request whose signature is missing, is not hex, was made over other bytes, or was made with another secret. It SHALL reply 404 for a workspace or connection that does not exist or does not receive, 400 when the request does not say what happened or its body is not JSON, and 413 when the body is larger than 5 MB. In every one of these cases no event SHALL be recorded and no run started.

#### Scenario: A tampered body
- **WHEN** a request arrives with a signature made over a different body
- **THEN** the reply is 401
- **AND** the workspace has no new event and no new run

#### Scenario: An address that receives nothing
- **WHEN** a signed request is posted to `/v1/webhooks/acme/gitlab`, and `acme` has no such connection
- **THEN** the reply is 404

### Requirement: A verified webhook becomes an event
Weir SHALL turn an accepted request into an event in the connection's workspace. Its type SHALL be `<connection>.<event>`, with the event name in lower case, its source the connection's name, and its data the request's body. Triggers SHALL match it like any other event, and a run it starts SHALL have the requester `webhook:<connection>`. The reply SHALL be 202 with `received`, `duplicate` and `started`, the number of workflows it started.

#### Scenario: A trigger wants it
- **WHEN** a workflow has the trigger `event: github.pull_request` with `when: event.data.action == "opened"`, and such a webhook arrives
- **THEN** the reply holds `started: 1`
- **AND** the run's requester is `webhook:github`

#### Scenario: No trigger wants it
- **WHEN** a signed webhook arrives for a pull request that was closed
- **THEN** the reply is 202 with `started: 0`

### Requirement: The delivery id makes a repeat harmless
Weir SHALL keep the sender's delivery id as the event's id. A delivery that arrives again with the same event type and id SHALL start nothing new and SHALL be answered with `duplicate: true`. A sender that gives no delivery id cannot have its repeats recognised.

#### Scenario: The same delivery twice
- **WHEN** a webhook with `X-GitHub-Delivery: d-1` is delivered twice
- **THEN** the second reply holds `duplicate: true`
- **AND** the workspace still has one run

### Requirement: One workspace's secret does not verify another's webhooks
The shared secret SHALL be looked up for the workspace in the address and no other. A request signed with another workspace's secret SHALL be refused, and an event received by one workspace MUST NOT start a run in another.

#### Scenario: Signed by the other workspace
- **WHEN** a request to `/v1/webhooks/acme/github` is signed with `rival`'s secret
- **THEN** the reply is 401

#### Scenario: Each workspace receives its own
- **WHEN** a request to `/v1/webhooks/rival/github` is signed with `rival`'s secret
- **THEN** the reply is 202 and `acme` gains no run

### Requirement: A webhook whose secret is not set is refused
The shared secret SHALL be named by reference and never stored. When the secret cannot be found on the server, Weir SHALL reply 503 and record nothing, whatever the signature is.

#### Scenario: The secret was never set
- **WHEN** a connection names `env:NOT_SET` and a request arrives signed with an empty secret
- **THEN** the reply is 503
