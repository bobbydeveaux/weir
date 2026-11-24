# failure-handling Specification

## Purpose
Steps fail. This says what Weir does when one does: when it tries again, what each failure policy does to the run, how finished steps are undone, and how a person resumes or cancels a run that has stopped.

## Requirements

### Requirement: A failed attempt is retried while attempts remain
A step MAY set `retry.attempts`, the most times it is tried; without it a step is tried once. When an attempt fails with a failure that trying again could fix, and attempts remain, the engine SHALL make the step ready again and record `attempt.failed` and `step.retrying`. A failure that trying again cannot fix SHALL NOT be retried, however many attempts remain. When no attempt remains the step has failed for good, with the status `failed` and its error recorded.

#### Scenario: Two attempts, both fail
- **WHEN** a step with `retry: { attempts: 2 }` fails twice
- **THEN** the step is `failed` after 2 attempts

#### Scenario: A failure that trying again cannot fix
- **WHEN** a step with `retry: { attempts: 3 }` fails because the server cannot give its agent a shell
- **THEN** the step is tried once and no more

### Requirement: Each retry waits longer than the last
The engine SHALL wait before each retry, and SHALL double the wait each time. The first wait is the step's `retry.backoff`, or two seconds when the step sets none.

#### Scenario: Three attempts with a backoff of ten seconds
- **WHEN** a step with `retry: { attempts: 3, backoff: 10s }` fails twice
- **THEN** the second attempt is not started for 10 seconds, and the third not for 20 seconds after that

### Requirement: A step that fails for good blocks the run by default
A step's `on_failure` SHALL be `block`, `cancel` or `continue`, and is `block` when left out. With `block`, the engine SHALL set the run to `blocked`, record the step's error on the run, and record `run.blocked`. Steps that had already finished SHALL keep their results.

#### Scenario: A step runs out of attempts
- **WHEN** the second step of a run fails for good with the default policy
- **THEN** the run is `blocked`
- **AND** the first step is still `succeeded`

### Requirement: A step with on_failure continue is passed over
With `on_failure: continue`, the engine SHALL leave the failed step as `failed`, with its error recorded, and SHALL move the run on as if the step had finished: by its `next` rules, else to the following step. The failed step has no output.

#### Scenario: An optional step fails
- **WHEN** a step with `on_failure: continue` fails, and another step follows it
- **THEN** the following step runs and the run is `succeeded`
- **AND** the first step is shown as `failed`

### Requirement: A step with on_failure cancel undoes finished steps in reverse
A step MAY name an undo handler in `compensate`. When a step with `on_failure: cancel` fails for good, the engine SHALL set the run to `compensating` and run the undo handler of every finished step that names one, one at a time, the most recently finished first. When nothing is left to undo, the run SHALL be `failed`, with the error of the step that failed.

#### Scenario: What an undo handler is given
- **WHEN** the undo handler for a finished step runs
- **THEN** it is given the input and the output of the step it undoes
- **AND** once it finishes, that step has the status `compensated`

#### Scenario: An undo handler on a step that is not deterministic
- **WHEN** a definition sets `compensate` on a step of any kind but `deterministic`
- **THEN** publishing is refused

#### Scenario: The third step fails
- **WHEN** steps `one` and `two` finish, both with an undo handler, and step `three` fails with `on_failure: cancel`
- **THEN** the undo for `two` runs, then the undo for `one`
- **AND** `one` and `two` are `compensated`
- **AND** the run is `failed` with the error from `three`

### Requirement: An undo handler that fails ends the run
An undo handler is tried once. When it fails, the engine SHALL end the run as `failed` with the error type `compensation_failed`, and SHALL NOT run the undo handlers that were still to come.

#### Scenario: The undo for a step fails
- **WHEN** a run is undoing its steps and the undo handler for `two` fails
- **THEN** the run is `failed` with the error type `compensation_failed`
- **AND** the undo for `one` does not run

### Requirement: A blocked run can be resumed from where it stopped
Resuming a `blocked` run SHALL record `run.resumed` with who resumed it, clear the run's error, and make the step that failed ready again with as many fresh attempts as the step allows. Steps that had finished SHALL NOT run again. Resuming a run that is not blocked SHALL be refused: the error is `ErrNotBlocked`, and 409 over the API.

#### Scenario: Resuming after a failure is put right
- **WHEN** a run is blocked at its second step, and a person resumes it
- **THEN** the second step runs again and the run can finish
- **AND** the first step is not run again

#### Scenario: Resuming a run that has finished
- **WHEN** a person resumes a run that has `succeeded`
- **THEN** the resume is refused because the run is not blocked

### Requirement: A run that spent its budget is resumed with a larger one
A resume MAY carry a new budget in dollars (`budget_usd` over the API), which replaces the run's budget. A run that has spent its budget SHALL NOT be resumed without a budget larger than what it has spent: the resume is refused as not allowed, and the run stays `blocked`. A run that was blocked on its budget after a step finished SHALL carry on from the step that comes next.

#### Scenario: No more money
- **WHEN** a run blocked with `budget_exceeded` is resumed with no new budget
- **THEN** the resume is refused as not allowed

#### Scenario: A larger budget
- **WHEN** the same run, which spent $6, is resumed with a budget of $20
- **THEN** the run carries on from the next step and can finish

### Requirement: A run that has not finished can be cancelled
A person SHALL be able to cancel a run that is not `succeeded`, `failed` or `cancelled`, with a reason. The engine SHALL set the run to `cancelled` and record `run.cancelled` with who cancelled it and why. Nothing more of the run SHALL run. Cancelling a run that has finished SHALL be refused as not allowed, 403 over the API.

#### Scenario: What is left of a cancelled run
- **WHEN** a run with a step in progress and an open human task is cancelled
- **THEN** its live attempts are given up, so their tokens are stale
- **AND** its open human tasks are closed, and its unfinished steps are `cancelled`
- **AND** steps that had already succeeded are not undone

#### Scenario: Cancelling a blocked run
- **WHEN** a person cancels a blocked run with the reason `cannot be put right`
- **THEN** the reply is 200 and the run is `cancelled`
- **AND** it can no longer be resumed

#### Scenario: Cancelling twice
- **WHEN** a person cancels a run that is already `cancelled`
- **THEN** the reply is 403

### Requirement: Output that fails its JSON Schema fails the attempt
When a step declares an `output` schema, the engine SHALL check every output against it, whoever sent it: a worker, a model or a third party's callback. An output that does not fit SHALL fail the attempt with the error type `output_invalid`, and SHALL NOT become the step's output. The failure is one that trying again could fix, so the step is retried if attempts remain.

#### Scenario: A field is missing
- **WHEN** an `ai` step's schema requires `branch`, and the agent returns `{"wrong": 1}`
- **THEN** the step is `failed` with the error type `output_invalid`
- **AND** with the default policy the run is `blocked`
