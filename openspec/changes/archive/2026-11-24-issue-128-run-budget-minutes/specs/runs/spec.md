## MODIFIED Requirements

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
