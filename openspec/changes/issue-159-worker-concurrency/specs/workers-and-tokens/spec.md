## ADDED Requirements

### Requirement: A worker runs up to its concurrency of steps at once
The worker that ships with Weir SHALL run up to its concurrency of steps at the same time, 1 unless set otherwise, each claimed exactly once. It SHALL claim a step only when it has a free slot, so it never holds a step it cannot start. When a step ends in an error it cannot recover from, it SHALL stop claiming, let the steps it holds finish, and return that error.

#### Scenario: Several steps at once
- **WHEN** a worker with a concurrency of 4 is given twelve runs whose steps each take a while
- **THEN** four steps are running at the same moment
- **AND** every step runs once, and every run is `succeeded`

#### Scenario: The default
- **WHEN** a worker with no concurrency set is given several runs
- **THEN** no two of their steps are running at the same moment

## MODIFIED Requirements

### Requirement: A worker holds a step under a lease and extends it with heartbeats
A claim SHALL give the worker a lease: a time by which it must report back. A worker SHALL be able to extend the lease by sending a heartbeat with the attempt's token. A heartbeat for an attempt that is no longer running SHALL be refused with `ErrStaleToken`. The worker that ships with Weir takes a lease of 30 seconds and, for each step it is running, sends a heartbeat every third of it until that step ends.

#### Scenario: A long step
- **WHEN** a step takes longer than its lease, and the worker sends heartbeats throughout
- **THEN** the lease does not run out, and the step is not given to anyone else

#### Scenario: Several long steps at once
- **WHEN** a worker runs several steps at once, each taking longer than its lease
- **THEN** no lease runs out, and no step is given to anyone else or run twice

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
