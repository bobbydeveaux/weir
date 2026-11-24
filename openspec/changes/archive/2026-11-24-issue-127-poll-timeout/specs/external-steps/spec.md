## ADDED Requirements

### Requirement: A poll step stops at its timeout
When a poll's condition is false and the step's `timeout` has passed since the step began polling, the engine SHALL NOT make the call again. It SHALL close the attempt as `timed_out` and fail the step with the error type `timeout`, and the step's `on_failure` SHALL decide what happens to the run. The timeout is checked only when a poll comes back, so a step MAY run over it by up to one `poll.every`.

#### Scenario: The third party never finishes
- **WHEN** a poll step has `timeout: 60ms` and `poll: { every: 5ms, until: response.status == "done" }`, and the third party always answers `pending`
- **THEN** the step fails with the error type `timeout`
- **AND** the run is `blocked` with the error type `timeout`
- **AND** the third party is not called again

#### Scenario: A step that timed out is resumed
- **WHEN** a run blocked by a poll step's timeout is resumed
- **THEN** the step polls again for its whole timeout, counted from the first poll after the resume

#### Scenario: The poll finishes in time
- **WHEN** a poll step has `timeout: 1h`, and the third party answers `pending` twice and then `done`
- **THEN** the step succeeds, having used one attempt

#### Scenario: No timeout is set
- **WHEN** a definition whose poll step sets no `timeout` is published
- **THEN** publishing accepts it, and the step polls until its condition holds

## MODIFIED Requirements

### Requirement: A poll step repeats the call until its condition holds
With `resume: poll`, the step SHALL make its call, read the reply as JSON, and evaluate `poll.until` with the reply as `response`. When the condition is true the step SHALL succeed with that reply as its output. When it is false the step SHALL make the call again after `poll.every`, or after five seconds when none is given, unless its timeout has passed. A poll that is not yet done SHALL NOT use up one of the step's retries.

#### Scenario: The third answer is the one
- **WHEN** a third party answers `pending` twice and then `done`, and the step has `until: response.status == "done"`
- **THEN** the third party is called three times
- **AND** the step succeeds with the third reply as its output, having used one attempt
