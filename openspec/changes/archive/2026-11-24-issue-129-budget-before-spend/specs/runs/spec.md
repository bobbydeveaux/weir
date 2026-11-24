## ADDED Requirements

### Requirement: A model call may spend no more than its run has left
A worker that is handed an ai step of a run with a budget SHALL allow the step's model call no more than the run has left, even when the step's own budget is larger. A call that cost more than it was allowed SHALL fail its step with the error type `budget_exceeded`, the step SHALL NOT be tried again, and the cost SHALL still be counted. A decide step's call is not limited in this way.

#### Scenario: The run has less than the step
- **WHEN** a run has a budget of $1 and its first step is an ai step with a budget of $5
- **THEN** the step's model call is told to stop at $1

#### Scenario: A runner that cannot stop itself
- **WHEN** that call comes back having cost $2
- **THEN** the step fails with the error type `budget_exceeded`, and its error names the run's budget
- **AND** the step is not tried again, the run is `blocked` with `budget_exceeded`, and its `spent_usd` is 2

## MODIFIED Requirements

### Requirement: A run's budget is a limit on what it spends
A definition MAY set `budget.usd`. The engine SHALL add the cost of every model call to the run's spend, including calls made by attempts that failed. When a step finishes and the run has spent more than its budget, the engine SHALL set the run to `blocked` with the error type `budget_exceeded`, and no further step SHALL run. A run with no budget has no limit.

#### Scenario: A step spends more than the budget
- **WHEN** a run has a budget of $5 and its first step, a decide step, costs $6
- **THEN** the run is `blocked` with the error type `budget_exceeded`
- **AND** the step after it does not run

#### Scenario: Spend is shown on the run
- **WHEN** a run's only model call cost $3.20
- **THEN** the run shows `spent_usd` as 3.20

### Requirement: The budget is checked between steps
The engine SHALL NOT hand out a step of a run whose budget is already spent: that step fails with the error type `budget_exceeded`. The budget is checked when a step finishes and before the next is handed out. The engine MUST NOT interrupt a step that is already running: what that step's model call may spend is settled before the call is made.

#### Scenario: A step is ready and the money is gone
- **WHEN** a run has spent its whole budget, and a worker claims its next step
- **THEN** the worker is not given the step, and the step fails with `budget_exceeded`

#### Scenario: A step is running when the budget is passed
- **WHEN** a running decide step's model call takes the run past its budget
- **THEN** the step finishes, and the run is blocked afterwards
