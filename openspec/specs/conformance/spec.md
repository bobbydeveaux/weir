# conformance Specification

## Purpose

The conformance suite states Weir's contract as data: for a definition and its inputs, the event log a run must produce. It stops the log changing unnoticed, and gives another implementation something to check itself against.

## Requirements

### Requirement: A conformance case is data
Each folder under `conformance/cases/` SHALL be one case, made of three files: `workflow.yaml`, a definition; `case.yaml`, the inputs, the scripted model, the replies of each third party, what is done to the run from outside, and the status and error it must end with; and `events.txt`, the event log the run must produce. A case SHALL use only handlers built in to Weir.

#### Scenario: A case is read
- **WHEN** the suite runs
- **THEN** every folder under `conformance/cases/` that holds a `case.yaml` is run as a case

#### Scenario: A field that is not known
- **WHEN** a `case.yaml` holds a field the runner does not know
- **THEN** the case fails, and the error names the field

### Requirement: A run's event log matches its case
For each case Weir SHALL publish the definition as written, do each outside step in order, and produce a run whose status, error type and event log are exactly those the case states. The event log is compared as the type of each event and the step it is about, in order.

#### Scenario: The suite passes
- **WHEN** every case is run against the engine and a real Postgres
- **THEN** each run ends with the status and error type its case states
- **AND** its event log is, line for line, the case's `events.txt`

#### Scenario: A log differs
- **WHEN** a run records an event its case does not list, omits one it does, or records them in another order
- **THEN** the case fails and shows both logs

### Requirement: A case's log can be recorded
With `WEIR_CONFORMANCE_RECORD=1` the suite SHALL write the event log each case produced to its `events.txt` in place of checking it, so that a new case can be written.

#### Scenario: Recording
- **WHEN** the suite is run with `WEIR_CONFORMANCE_RECORD=1`
- **THEN** each case's `events.txt` holds the log its run produced
