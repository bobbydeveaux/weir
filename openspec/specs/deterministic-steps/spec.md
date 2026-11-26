# deterministic-steps Specification

## Purpose
A deterministic step runs a piece of registered code, called a handler. It is how a workflow does plain work that needs no model and no person: open an issue, send a message, commit a change.

## Requirements

### Requirement: A deterministic step runs a registered handler
A deterministic step SHALL name a handler. When the step runs, the worker SHALL call that handler, and what the handler returns SHALL become the step's output, which later steps read as `steps.<id>.output`.

#### Scenario: A handler's result is the step's output
- **WHEN** a run reaches a deterministic step whose handler returns `{"made": "one"}`
- **THEN** the step succeeds with that output
- **AND** the run moves on to the next step

### Requirement: A handler is given the step's input, the run and the step
A handler SHALL be given the step's input as JSON, after the step's `input` expressions have been worked out, together with the id of the run and the id of the step.

#### Scenario: A handler reads what it was given
- **WHEN** a handler runs for the step `two`
- **THEN** the step it is told about is `two`
- **AND** the input it is given is the JSON the step's `input` resolved to

### Requirement: A handler is given the same idempotency key on every attempt
Every call to a handler SHALL carry an idempotency key. The key MUST be the same for every attempt at one visit to a step, so that a handler with side effects can make a retry safe.

#### Scenario: A step is tried twice
- **WHEN** a handler fails and the step is tried again
- **THEN** the second call carries the idempotency key the first one did

### Requirement: Publishing refuses a handler that is not registered
Publishing SHALL refuse a definition whose deterministic step names no handler, or names a handler that is not registered on the server. The error SHALL name the step and the handler.

#### Scenario: Nobody can run the handler
- **WHEN** a definition with the handler `nobody.home` is published
- **THEN** publishing is refused, and the error says the handler is not registered

### Requirement: A handler's error is retried unless it is marked permanent
When a handler returns an error, the attempt SHALL fail with the error type `handler_failed` and the handler's own message. The step SHALL be tried again while it has attempts left, unless the handler marked the error as permanent, in which case the step fails at once.

#### Scenario: A passing fault
- **WHEN** a handler fails twice on a step with `retry: { attempts: 2 }`
- **THEN** the step is tried twice and then fails
- **AND** the run is `blocked`

#### Scenario: A permanent error
- **WHEN** a handler returns a permanent error
- **THEN** the step fails with the error type `handler_failed`
- **AND** the step is not tried again

### Requirement: A worker without the handler fails the step
When a worker claims a deterministic step and does not have its handler, the step SHALL fail with the error type `handler_missing`. It SHALL NOT be tried again.

#### Scenario: The handler is gone when the step runs
- **WHEN** a worker claims a step whose handler it does not have
- **THEN** the step fails with the error type `handler_missing`

### Requirement: Two handlers are built in
Every server SHALL register `core.noop`, which returns an empty object, and `core.echo`, which returns the input it was given.

#### Scenario: A step that does nothing
- **WHEN** a run reaches a step with the handler `core.noop`
- **THEN** the step succeeds

### Requirement: A handler's output is checked against the step's schema
When a deterministic step declares an `output` schema, the engine SHALL check the handler's output against it. Output that does not match SHALL fail the attempt with the error type `output_invalid`.

#### Scenario: A handler returns the wrong shape
- **WHEN** a handler returns output that does not match the step's output schema
- **THEN** the attempt fails with the error type `output_invalid`

### Requirement: A deterministic step takes no field of another kind
Publishing SHALL refuse a deterministic step that sets a field belonging to another kind of step, such as `prompt` or `labels`.

#### Scenario: A prompt on a deterministic step
- **WHEN** a definition whose deterministic step also sets `prompt` is published
- **THEN** publishing is refused, and the error says `prompt` is not a field of a deterministic step

### Requirement: The server lists its registered handlers
`GET /v1/registry` SHALL list the names of the handlers registered on the server, so that a person writing a workflow can see what a deterministic step may name.

#### Scenario: Reading the registry
- **WHEN** the registry is read
- **THEN** its `handlers` list holds `core.echo` and `core.noop`

### Requirement: A handler can ask to be run again later
A handler MAY answer that its work is not ready, with a delay. The step SHALL then be queued to run again after that delay, with the same input and idempotency key, and the answer SHALL NOT use up one of the step's attempts. When the step sets a `timeout` and that long has passed since the step first ran, or since it last failed, the engine SHALL NOT run the handler again and SHALL fail the step with the error type `timeout`.

#### Scenario: Ready the third time
- **WHEN** a handler answers "not yet" twice and then returns an output
- **THEN** the handler ran three times, and the step succeeds having used one attempt

#### Scenario: Never ready
- **WHEN** a step has `timeout: 60ms` and its handler always answers "not yet" with a delay of 5ms
- **THEN** the step fails with the error type `timeout`, and the run is `blocked`
- **AND** the handler is not run again

#### Scenario: No timeout is set
- **WHEN** a step sets no `timeout` and its handler answers "not yet"
- **THEN** the handler is run again after the delay, for as long as it gives that answer
