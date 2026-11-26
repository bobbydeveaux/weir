## ADDED Requirements

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
