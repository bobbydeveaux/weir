# hooks Specification

## Purpose
A hook tells an application when a run moves. A workflow says which events it wants reported and where to send them, and Weir delivers each one, signed if asked, until it arrives or Weir gives up.

## Requirements

### Requirement: A workflow declares its hooks
A definition MAY have a `hooks:` block. Each hook SHALL have `on`, a list of what to report; `connection`, where to send it; and `path`, starting with a slash. It MAY have `secret`, a reference to a shared secret to sign with. Publishing SHALL refuse a hook that is not well formed, or whose connection does not exist in the workspace.

#### Scenario: What on may hold
- **WHEN** a hook lists an event's name such as `run.succeeded`, a family such as `run.*`, or `*`
- **THEN** the definition is published

#### Scenario: A hook that is not well formed
- **WHEN** a hook has no connection, a path with no leading slash, an empty or malformed `on`, or a secret that is not `env:NAME` or `file:name`
- **THEN** publishing is refused

#### Scenario: A hook to an unknown connection
- **WHEN** a definition with a hook to the connection `app` is published, and the workspace has no `app`
- **THEN** publishing is refused

#### Scenario: A malformed name in on
- **WHEN** a hook lists `Everything!` under `on`
- **THEN** publishing is refused

### Requirement: A hook reports each event it asked for
When a run of the workflow records an event that a hook's `on` matches, Weir SHALL queue one hook for it, together with the event, so that a hook exists if and only if the thing it reports happened. The hook's body SHALL be JSON with the event's `type`, the `run_id`, the event's `seq`, the `step` (when the event is a step's), the `actor`, the `workflow` (namespace, name and version), the run's input as `run_input`, the event's `data`, and the time as `at`.

#### Scenario: A run waits for a person
- **WHEN** a workflow has a hook on `run.*` and `step.waiting`, and its run reaches a human step and later succeeds
- **THEN** the application receives `step.waiting`, `run.started` and `run.succeeded`, and nothing else
- **AND** the `step.waiting` hook names the step, the run and the run's input

### Requirement: The worker delivers hooks through the connection
The worker SHALL deliver each queued hook as a `POST` of its body to the connection's address followed by the hook's path, with the connection's own credential added. Each request SHALL carry `Weir-Hook-Id`, which identifies the hook and is the same on every attempt at it, so a receiver that has seen it can ignore a repeat. A reply with a status below 300 SHALL count as delivered.

#### Scenario: Three hooks, three ids
- **WHEN** three hooks of one run are delivered
- **THEN** each arrives with a different `Weir-Hook-Id`

#### Scenario: The connection has a credential
- **WHEN** a hook goes to a connection with a bearer credential
- **THEN** the request carries `Authorization: Bearer` and the secret

### Requirement: A failed delivery is retried with a growing wait
When a delivery fails or the receiver answers 300 or above, Weir SHALL keep the hook and try it again later, waiting longer each time: five seconds after the first failure, then double the wait after each one. Other hooks SHALL NOT wait behind it. After eight attempts Weir SHALL give up on the hook and mark it failed.

#### Scenario: The receiver refuses once
- **WHEN** the application answers 503 to the first hook it is sent
- **THEN** that hook is held back and the next one is delivered
- **AND** once its wait has passed the held hook is delivered, with two attempts recorded

### Requirement: A failed hook does not fail the run
A hook that cannot be delivered MUST NOT change the run it reports on. The run SHALL go on and finish as it would with no hooks.

#### Scenario: The first hook is refused
- **WHEN** the application refuses the `run.started` hook, and the run's task is then approved
- **THEN** the run succeeds

### Requirement: A hook can be signed so the receiver knows it is from Weir
A hook that names a `secret` SHALL be sent with two more headers: `Weir-Timestamp`, the time it was sent in seconds since 1970, and `Weir-Signature`, which is `sha256=` followed by the hex HMAC-SHA256 of the timestamp, a full stop and the exact bytes of the body, under the shared secret. The secret SHALL be read for the workflow's own workspace when the hook is sent, and MUST NOT be stored with the queued hook. A hook with no `secret` SHALL be sent with neither header.

#### Scenario: A signed hook
- **WHEN** a hook with `secret: env:HOOKS` is delivered
- **THEN** the receiver can work out the same signature from the timestamp, the body and the shared secret
- **AND** the timestamp is the current time

#### Scenario: A hook with no secret
- **WHEN** a hook that names no secret is delivered
- **THEN** it carries no `Weir-Signature` and no `Weir-Timestamp`

### Requirement: A hook whose secret is not set is not sent unsigned
When a hook names a secret that cannot be found on the server, Weir MUST NOT send it. The hook SHALL wait and be tried again like any failed delivery, and its recorded error SHALL name the variable or file to set.

#### Scenario: The secret was never set
- **WHEN** a hook names `env:NOT_SET` in the default workspace and its run succeeds
- **THEN** the application receives nothing for it
- **AND** the hook's error names `WEIR_SECRET__DEFAULT__NOT_SET`

### Requirement: Hooks are listed in a run's record
`GET /v1/runs/{id}/record` SHALL list the run's hooks in the order they were queued, each with the event `type`, the `step`, the `connection`, the `path`, the number of `attempts`, when it was queued, when it was delivered or given up on, and its last `error`.

#### Scenario: A run with three hooks
- **WHEN** a run's three hooks have been delivered, the first on its second attempt
- **THEN** the record lists three hooks, the first with the type `run.started` and two attempts, and the last with a delivery time
