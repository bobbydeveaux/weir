# runs Specification

## Purpose
A run is one execution of a workflow. This says how a run starts, which statuses it can have, how it moves from one step to the next, how it ends, and how its budget in dollars limits what it spends.

## Requirements

### Requirement: A run starts on the live version, with an input and a requester
Starting a run SHALL name a workflow by namespace and name, and SHALL use the version the `live` alias points at. The run's input is JSON, and is `{}` when none is given. A run MUST have a requester: who or what started it. A new run SHALL have the status `running`, its first event SHALL be `run.started`, and its first step SHALL be the first step in the definition. Starting a workflow that does not exist in the workspace SHALL be refused as not found.

#### Scenario: Starting over the API
- **WHEN** alice posts `{"namespace": "shop", "name": "refund", "input": {"order": "A-17"}}` to `/v1/runs`
- **THEN** the reply is 201, with the new run's id
- **AND** the run's event log begins with `run.started`

#### Scenario: A workflow the workspace does not have
- **WHEN** a workspace starts a workflow that only another workspace has published
- **THEN** no run is started

### Requirement: A run's input is checked against the definition's input schema
When a definition declares an `input` schema, the engine SHALL check a run's input against it before the run is created. An input that does not fit SHALL be refused, and no run SHALL be started.

#### Scenario: An input that does not fit
- **WHEN** a definition's input schema requires `order`, and a run is started with `{}`
- **THEN** the start is refused, and the error is about the run's input
- **AND** no run exists

### Requirement: The same idempotency key returns the same run
A start MAY carry an idempotency key. Starting the same workflow again with the same key SHALL start nothing, and SHALL return the run the first start made. Over the API the first reply is 201 with `created: true`, and a repeat is 200 with `created: false`.

#### Scenario: Starting twice
- **WHEN** a workflow is started twice with the idempotency key `order-42`
- **THEN** the second start returns the first run's id
- **AND** only one run exists

### Requirement: A run has one of seven statuses
A run's status SHALL be one of: `running`, when a step is ready or being worked on; `waiting`, when its current step waits for a person or a callback; `blocked`, when it has stopped and a person must resume or cancel it; `compensating`, while finished steps are being undone; and `succeeded`, `failed` or `cancelled`, which are final. A run in a final status MUST NOT be resumed or cancelled.

#### Scenario: A run reaches a human step
- **WHEN** a run reaches a human step
- **THEN** its status is `waiting`

#### Scenario: A run's last step finishes
- **WHEN** the last step of a run succeeds
- **THEN** its status is `succeeded`

### Requirement: The first matching next rule decides the next step
When a step finishes, the engine SHALL read its `next` rules in the order they are written and go to the step named by the first rule that matches. A rule with a `when` matches when the condition is true. A rule with no `when` always matches.

#### Scenario: The first rule matches
- **WHEN** a decide step chooses `fix`, and its rules are `when: label == "fix"` going to `open_issue`, then a rule with no `when` going to `needs_human`
- **THEN** the run goes to `open_issue`

#### Scenario: Only the last rule matches
- **WHEN** the same step chooses `needs_human`
- **THEN** the run goes to `needs_human`

### Requirement: With no matching rule a run goes to the following step
When a step has no `next` rules, or none of them matches, the run SHALL go to the step written after it in the definition.

#### Scenario: A step with no rules
- **WHEN** the step `open_issue` has no `next` rules and is followed by `fix`
- **THEN** the run goes to `fix`

#### Scenario: A rule that does not match
- **WHEN** a human step has only the rule `when: outcome == "rejected"`, and the person approves
- **THEN** the run goes to the step written after it

### Requirement: A run ends at a step marked end, or after the last step
A step with `end: true` SHALL NOT fall through to the step written after it: unless one of its own `next` rules matches, the run ends there. A run also ends when its last step finishes with no rule matching. A run that ends this way SHALL have the status `succeeded`, its output SHALL be the output of the step it ended on, and its last event SHALL be `run.succeeded`.

#### Scenario: Ending before the last step
- **WHEN** the step `verify` has `end: true`, and a step `needs_human` is written after it
- **THEN** the run is `succeeded` when `verify` finishes
- **AND** `needs_human` never runs

### Requirement: Each time a run reaches a step is a new visit
A `next` rule MAY send a run back to a step it has already been through. Each time a run reaches a step, the engine SHALL record a new visit, numbered from 1, with its own input, attempts and output. Earlier visits SHALL be kept and shown with the run.

#### Scenario: A run goes round again
- **WHEN** a rule sends a run back to the step `fix` after it has finished once
- **THEN** the run shows `fix` twice, as visit 1 and visit 2

### Requirement: A run's budget is a limit on what it spends
A definition MAY set `budget.usd`. The engine SHALL add the cost of every model call to the run's spend, including calls made by attempts that failed. When a step finishes and the run has spent more than its budget, the engine SHALL set the run to `blocked` with the error type `budget_exceeded`, and no further step SHALL run. A run with no budget has no limit. No run has a time limit: a definition's top-level `budget.minutes` is not acted on.

#### Scenario: A step spends more than the budget
- **WHEN** a run has a budget of $5 and its first step costs $6
- **THEN** the run is `blocked` with the error type `budget_exceeded`
- **AND** the step after it does not run

#### Scenario: Spend is shown on the run
- **WHEN** a run's only model call cost $3.20
- **THEN** the run shows `spent_usd` as 3.20

#### Scenario: A run takes longer than the minutes in its budget
- **WHEN** a definition's top-level budget sets `minutes: 30` and a run of it lasts longer
- **THEN** the run is not stopped for it

### Requirement: The budget is checked between steps
The engine SHALL NOT hand out a step of a run whose budget is already spent: that step fails with the error type `budget_exceeded`. The budget is checked when a step finishes and before the next is handed out; a step that is already running MUST NOT be interrupted by it.

#### Scenario: A step is ready and the money is gone
- **WHEN** a run has spent its whole budget, and a worker claims its next step
- **THEN** the worker is not given the step, and the step fails with `budget_exceeded`

#### Scenario: A step is running when the budget is passed
- **WHEN** a running step's model call takes the run past its budget
- **THEN** the step finishes, and the run is blocked afterwards
