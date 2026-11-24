# audit-log Specification

## Purpose
Weir keeps a record of everything a run did, which nobody can rewrite afterwards: an ordered event log, every model call and every tool call. A run can also be followed live, as its events happen.

## Requirements

### Requirement: Every run has an ordered event log
The engine SHALL record an event for each thing that happens to a run. Each event SHALL carry a sequence number that counts up from 1 within its run with no gaps, its type, the actor who caused it, the step it is about if any, and data. Reading a run SHALL return its events in order.

#### Scenario: The types of event
- **WHEN** something happens to a run, a step, an attempt or a task
- **THEN** the event's type is one of `run.started`, `run.blocked`, `run.resumed`, `run.succeeded`, `run.failed` and `run.cancelled`
- **AND** or one of `step.ready`, `step.waiting`, `step.succeeded`, `step.retrying`, `step.requeued`, `step.deferred`, `step.failed` and `step.compensating`
- **AND** or one of `attempt.started`, `attempt.failed`, `attempt.timed_out`, `attempt.abandoned` and `task.escalated`

#### Scenario: A run of two steps
- **WHEN** a run of two deterministic steps finishes
- **THEN** its log is `run.started`, then `step.ready`, `attempt.started` and `step.succeeded` for each step, then `run.succeeded`

#### Scenario: Who did it
- **WHEN** bob approves a task
- **THEN** the `step.succeeded` event for that step has the actor `bob`

### Requirement: An event is written with the change it records
The engine SHALL write a change to a run or a step and the event that records it together: both are kept, or neither is. A run MUST NOT show a change that has no event, or an event for a change that did not happen.

#### Scenario: A change fails half-way
- **WHEN** completing an attempt fails after the step was updated and before the event was written
- **THEN** the step is as it was before, and the log has no new event

### Requirement: The event log cannot be changed or deleted
An event, once written, MUST NOT be changed or deleted. The database SHALL refuse both itself, whoever asks and by whatever means.

#### Scenario: Someone rewrites the log in the database
- **WHEN** an event's actor is updated with SQL, or the events are deleted
- **THEN** the database refuses

### Requirement: Published versions cannot be changed or deleted
A published version of a definition, and a published version of a template, MUST NOT be changed or deleted. The database SHALL refuse both itself. So the version a run ran on, and the prompt it was given, can always be read back as they were.

#### Scenario: Someone edits a published definition in the database
- **WHEN** a definition version's source is updated with SQL, or the versions are deleted
- **THEN** the database refuses

### Requirement: Every model call is recorded
For each model call an attempt makes, whether the attempt goes on to succeed or fail, the engine SHALL record: the runner, the provider and the model; the template's key and version; a hash of the prompt; the prompt as sent and the reply as received; tokens in and out; the cost in dollars, and whether it was measured or estimated; how long the call took; and its error, if it failed.

#### Scenario: A very long prompt or reply
- **WHEN** a call's prompt or reply is longer than 200,000 bytes
- **THEN** the record holds its first 200,000 bytes, with a note of its full length

#### Scenario: A decide step runs
- **WHEN** a decide step using the template `classify@1` finishes
- **THEN** the run's record holds one model call, with the template `classify`

#### Scenario: An agent's call
- **WHEN** an `ai` step's agent finishes a call that cost $0.02
- **THEN** the run's record holds the call's prompt and its cost

### Requirement: Every tool call is recorded, allowed or refused
For each tool call an agent makes, the engine SHALL record the tool, whether it is a read or a write, the arguments, the result or the error, how long it took, and a status: `succeeded`, `failed`, or `denied` for a tool the step did not list. A result larger than 16 KB SHALL be replaced in the record by a note of its size.

#### Scenario: One call of each kind
- **WHEN** an agent calls a listed tool that works, the same tool with arguments that make it fail, and a tool its step did not list
- **THEN** the record holds three calls: `succeeded`, `failed` and `denied`

### Requirement: A run's record can be read in one request
`GET /v1/runs/{id}/record` SHALL return what a run's steps did beyond their outputs: every model call and every tool call, each tied to its step and visit; each step's input, times and spend; the time of each event; and each hook the run queued, with whether it was delivered. A request from another workspace SHALL get 404.

#### Scenario: Reading the record
- **WHEN** a workspace reads the record of one of its runs
- **THEN** the reply holds `ai_calls`, `tool_calls`, `step_times`, `event_times` and `hooks`

#### Scenario: Another workspace asks
- **WHEN** a different workspace asks for the same record
- **THEN** the reply is 404

### Requirement: A run can be followed as a live stream
`GET /v1/runs/{id}/stream` SHALL answer as Server-Sent Events. It SHALL first send every event the run already has, in order, and then each new event as it happens, without the client asking again. Each event is sent with its sequence number as its `id`. The stream stays open until the client closes it. A run in another workspace SHALL get 404.

#### Scenario: Following a run that waits for a person
- **WHEN** a client follows a run that has started and is waiting at a human step
- **THEN** it is sent `run.started` with id 1 and `step.waiting` with id 2
- **AND** when the task is decided, it is sent `step.succeeded` with id 3

### Requirement: A stream carries on from where the client stopped
A client that reconnects SHALL be able to say the last sequence number it saw, in the `Last-Event-ID` header or as `?after=`. The stream SHALL then send only the events after that number, so that nothing is missed and nothing is sent twice.

#### Scenario: Reconnecting after the third event
- **WHEN** a client follows a run with `Last-Event-ID: 3`
- **THEN** the first event it is sent has id 4

### Requirement: A workspace stream says which runs have moved
`GET /v1/stream` SHALL answer as Server-Sent Events, and SHALL send the id of a run each time a run in the workspace has a new event. It carries no detail of the event and cannot be resumed: it is a prompt to look again. It MUST NOT say anything about a run in another workspace.

#### Scenario: A run in the workspace moves
- **WHEN** a task is decided in the workspace `acme`
- **THEN** a client following `acme` is sent the run's id
- **AND** a client following another workspace is sent nothing
