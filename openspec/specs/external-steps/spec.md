# external-steps Specification

## Purpose
An external step calls a third party through a named connection, then waits for the answer. The third party either calls back with a one-time token, or Weir asks again until a condition holds.

## Requirements

### Requirement: An external step calls a named connection
An external step SHALL name a `connection` and a `call`. When the step runs, Weir SHALL send one HTTP request to the connection's address followed by `call.path`, with the method in `call.method`, or `POST` when none is given, and a JSON body. The connection's credential SHALL be added to the request, and without it the call SHALL NOT be made. Publishing SHALL refuse a step that names no connection, declares no call, or names a connection the workspace does not have.

#### Scenario: A step calls a third party
- **WHEN** a run reaches an external step for the connection `detector` with `call: { path: /verify }`
- **THEN** the third party receives one request at `/verify`

#### Scenario: The connection's secret is not set
- **WHEN** an external step runs and its connection's secret cannot be found
- **THEN** no request is sent
- **AND** the step fails with the error type `secret_unavailable`

### Requirement: What is sent is the step's input
The body of the request SHALL be the step's `input`, worked out from its expressions. `call.body` is an older way to write the same thing and SHALL be sent in the same way. Publishing SHALL refuse a step that sets both. A step that sets neither SHALL send an empty object.

#### Scenario: Input and body are sent alike
- **WHEN** one step sets `input: { order: input.order }` and another sets `call.body: { order: input.order }`, and the run's input has the order `A-17`
- **THEN** each third party receives a body with the order `A-17`

#### Scenario: Both are set
- **WHEN** a definition whose external step sets both `input` and `call.body` is published
- **THEN** publishing is refused, and the error says what is sent is said twice

### Requirement: Every call carries an idempotency key
Every request an external step sends SHALL carry an `Idempotency-Key` header. The key MUST be the same for every attempt and every poll of one visit to the step, so that a third party can ignore a repeat.

#### Scenario: A call is retried
- **WHEN** an external step's call fails and is made again
- **THEN** both requests carry the same `Idempotency-Key`

### Requirement: A callback step hands the third party a one-time token
With `resume: callback`, the request SHALL carry the attempt's token in `Weir-Callback-Token`, and the address to return it to in `Weir-Callback-URL`: the server's public address followed by `/v1/callbacks/` and the token. Once the third party has accepted the request, the step SHALL wait and the run's status SHALL be `waiting`.

#### Scenario: The call is made and the step waits
- **WHEN** a callback step's request is accepted by the third party
- **THEN** the request carried a token in `Weir-Callback-Token`
- **AND** `Weir-Callback-URL` begins with the server's address and `/v1/callbacks/`
- **AND** the run is `waiting`

### Requirement: A callback completes the step once
`POST /v1/callbacks/{token}` SHALL complete the waiting step with the request's JSON body as the step's output, and move the run on. The token is the credential: the request needs no API key. A token that has been used, has expired or was never issued SHALL change nothing, and the reply SHALL be 409. A body that is not JSON SHALL be refused with 400. The engine SHALL check the body against the step's output schema as it does any output.

#### Scenario: The third party calls back
- **WHEN** the third party posts `{"refunded": true}` to the callback address it was given
- **THEN** the reply is 200 and the run moves on

#### Scenario: The same callback again
- **WHEN** the third party posts to the same address a second time
- **THEN** the reply is 409 and nothing changes

#### Scenario: A made-up token
- **WHEN** someone posts to `/v1/callbacks/not-a-real-token`
- **THEN** the reply is 409

### Requirement: A callback step must set a timeout
Publishing SHALL refuse a step with `resume: callback` that sets no `timeout`. When the timeout passes with no callback, the engine SHALL fail the attempt with the error type `timeout`.

#### Scenario: No timeout is set
- **WHEN** a definition whose callback step has no `timeout` is published
- **THEN** publishing is refused, and the error says a step that waits for a callback must set a timeout

### Requirement: A poll step repeats the call until its condition holds
With `resume: poll`, the step SHALL make its call, read the reply as JSON, and evaluate `poll.until` with the reply as `response`. When the condition is true the step SHALL succeed with that reply as its output. When it is false the step SHALL make the call again after `poll.every`, or after five seconds when none is given. A poll that is not yet done SHALL NOT use up one of the step's retries.

#### Scenario: The third answer is the one
- **WHEN** a third party answers `pending` twice and then `done`, and the step has `until: response.status == "done"`
- **THEN** the third party is called three times
- **AND** the step succeeds with the third reply as its output, having used one attempt

### Requirement: A resume mode is required
Publishing SHALL refuse an external step whose `resume` is neither `callback` nor `poll`, a poll step with no `poll.until`, and a callback step that sets `poll`.

#### Scenario: A poll step with no condition
- **WHEN** a definition whose external step has `resume: poll` and no `poll.until` is published
- **THEN** publishing is refused

### Requirement: A refused or failed call fails the attempt
When the request cannot be sent, or the third party answers with a status of 400 or above, the attempt SHALL fail with the error type `call_failed`. The message SHALL give the status and the reason the reply gives, read from `error`, `detail` or `message` in its JSON. The call SHALL be made again only where trying again could help, and while the step has attempts left.

#### Scenario: The third party refuses the request
- **WHEN** a third party answers 422 with `{"error": "unknown order"}`
- **THEN** the step fails with the error type `call_failed`, and the message holds 422 and "unknown order"
- **AND** the call is not made again, as for any status from 400 to 499 except 429

#### Scenario: The third party is briefly down
- **WHEN** a step with `retry: { attempts: 3 }` gets no connection, a status of 500 or above, or 429
- **THEN** the call is made again

#### Scenario: A poll reply that is not JSON
- **WHEN** a poll step's reply cannot be read as JSON
- **THEN** the attempt fails with the error type `response_invalid`
