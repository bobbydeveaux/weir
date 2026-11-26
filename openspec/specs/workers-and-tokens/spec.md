# workers-and-tokens Specification

## Purpose
Workers take steps from the engine, do the work and report back. This says how a step is claimed and held, how a one-time token guards each attempt, and what happens when a worker disappears or time runs out.

## Requirements

### Requirement: A worker claims one ready step at a time
A worker SHALL claim work by naming the kinds of step it runs. The engine SHALL hand it one ready step of those kinds, the one that has been ready longest, or nothing when none is ready. A claim starts a new attempt, numbered from 1 within the step, and records `attempt.started` with the worker's name. A step that one worker holds MUST NOT be handed to another. A human step is never handed to a worker.

#### Scenario: Two workers ask for the same step
- **WHEN** one worker has claimed a run's only step, and a second worker claims
- **THEN** the second worker is given nothing

#### Scenario: Nothing is ready
- **WHEN** a worker claims and no step of its kinds is ready
- **THEN** it is given nothing, and nothing changes

### Requirement: Each attempt has a one-time token, stored only as a hash
Each attempt SHALL have its own token, made at random. The engine SHALL give the token out once, to whoever holds the attempt, and SHALL store only its SHA-256 hash. A new attempt at the same step SHALL get a new token, so the token of an earlier attempt opens nothing.

#### Scenario: A step is tried again
- **WHEN** an attempt is given up and the step is claimed again
- **THEN** the new attempt has a different token
- **AND** the first token is refused as stale

### Requirement: A token finishes its attempt once
The holder of a live attempt's token SHALL be able to complete the attempt or fail it, once. A token that is stale, has been used, or was never issued SHALL be refused with the error `ErrStaleToken`, and MUST change nothing: no step, no run and no event. Over the callback endpoint the refusal is 409.

#### Scenario: A callback arrives twice
- **WHEN** a third party posts its result to `/v1/callbacks/{token}`, and then posts it again
- **THEN** the first reply is 200 and the second is 409
- **AND** the run is as the first callback left it

#### Scenario: A made-up token
- **WHEN** someone posts to `/v1/callbacks/not-a-real-token`
- **THEN** the reply is 409

### Requirement: A worker holds a step under a lease and extends it with heartbeats
A claim SHALL give the worker a lease: a time by which it must report back. A worker SHALL be able to extend the lease by sending a heartbeat with the attempt's token. A heartbeat for an attempt that is no longer running SHALL be refused with `ErrStaleToken`. The worker that ships with Weir takes a lease of 30 seconds and, for each step it is running, sends a heartbeat every third of it until that step ends.

#### Scenario: A long step
- **WHEN** a step takes longer than its lease, and the worker sends heartbeats throughout
- **THEN** the lease does not run out, and the step is not given to anyone else

#### Scenario: Several long steps at once
- **WHEN** a worker runs several steps at once, each taking longer than its lease
- **THEN** no lease runs out, and no step is given to anyone else or run twice

### Requirement: A vanished worker's step is run again without using a retry
When a running attempt's lease runs out, the sweep SHALL close the attempt as `abandoned`, record `attempt.abandoned` and `step.requeued`, and make the step ready again at once. The lost attempt SHALL NOT count towards the step's `retry.attempts`. The vanished worker's token is stale from then on.

#### Scenario: A worker dies in the middle of a step
- **WHEN** a worker claims a step that allows one attempt, and never reports back
- **THEN** after the lease runs out, the sweep finds one expired lease
- **AND** another worker runs the step, once, and the run is `succeeded`
- **AND** completing with the first worker's token is refused as stale

### Requirement: A step whose workers keep vanishing fails
The engine SHALL run a step again after its worker vanished at most three times. When a fourth attempt at the step is abandoned, the step SHALL fail for good with the error type `worker_lost`, and its failure policy decides what happens to the run.

#### Scenario: Four workers vanish
- **WHEN** four attempts at one step are abandoned in a row
- **THEN** the step is `failed` with the error type `worker_lost`

### Requirement: The sweep acts on everything time has caught up with
The sweep SHALL find every running attempt whose lease has run out, and every timer that has come due. A timeout timer fails its attempt, closing it as `timed_out`, with the error type `timeout`; this is not retried. A timer whose attempt has already finished SHALL do nothing. The sweep is safe to run from several places at once: each expired lease and each timer is acted on once. The worker that ships with Weir sweeps about once a second, including while every one of its slots is busy.

#### Scenario: A waiting step's timeout passes
- **WHEN** a waiting step's timeout passes, and the sweep runs
- **THEN** the sweep reports one timeout
- **AND** the step fails with the error type `timeout`

#### Scenario: The step finished first
- **WHEN** a step is completed before its timeout, and the sweep runs after the timeout has passed
- **THEN** nothing happens to the step or the run

#### Scenario: The worker is busy
- **WHEN** every slot of a worker is taken by a long step, and another worker's lease runs out
- **THEN** the busy worker's sweep closes the expired attempt as `abandoned` before its own steps finish

### Requirement: A step has at most one live attempt
At any moment a step SHALL have at most one attempt that is running or waiting. The database enforces this itself, so that no fault in a worker or the engine can leave two attempts at one step live together.

#### Scenario: A second attempt is forced in
- **WHEN** a step has a running attempt, and a second running attempt for it is inserted into the database
- **THEN** the database refuses

### Requirement: An attempt can wait, or go back in the queue, without using a retry
A worker SHALL be able to park a running attempt: the attempt becomes `waiting`, holds no lease, keeps its token, and the run becomes `waiting` until the token is returned or the step's timeout passes. A worker SHALL also be able to defer a running attempt: the step goes back in the queue to be claimed again after a delay, and the deferred attempt does not count towards the step's `retry.attempts`.

#### Scenario: A step waits for a callback
- **WHEN** an external step with `resume: callback` has made its call
- **THEN** the run is `waiting`
- **AND** completing with the token that was sent to the third party moves the run on

#### Scenario: A step polls three times
- **WHEN** an external step polls three times before its condition holds
- **THEN** the step shows one attempt

### Requirement: A worker runs up to its concurrency of steps at once
The worker that ships with Weir SHALL run up to its concurrency of steps at the same time, 1 unless set otherwise, each claimed exactly once. It SHALL claim a step only when it has a free slot, so it never holds a step it cannot start. When a step ends in an error it cannot recover from, it SHALL stop claiming, let the steps it holds finish, and return that error.

#### Scenario: Several steps at once
- **WHEN** a worker with a concurrency of 4 is given twelve runs whose steps each take a while
- **THEN** four steps are running at the same moment
- **AND** every step runs once, and every run is `succeeded`

#### Scenario: The default
- **WHEN** a worker with no concurrency set is given several runs
- **THEN** no two of their steps are running at the same moment
