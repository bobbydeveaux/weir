## ADDED Requirements

### Requirement: A step's budget covers all its attempts
A step's `budget.usd` SHALL be the most that all the attempts of one visit to the step may spend together. Each attempt SHALL be allowed only what the earlier attempts left. When less than a cent is left, the worker SHALL make no model call: the step SHALL fail with the error type `budget_exceeded` and SHALL NOT be tried again. Resuming the run SHALL NOT give the step more money.

#### Scenario: A second attempt has what the first left
- **WHEN** a step with a budget of $5 fails its first attempt having cost $3, and is tried again
- **THEN** the second attempt's model call is told to stop at $2

#### Scenario: Nothing is left
- **WHEN** the second attempt costs the $2 that was left and its output does not fit the schema, with a third attempt allowed
- **THEN** no third model call is made
- **AND** the step fails with the error type `budget_exceeded`, and its error says that no call was made and names the step's budget

#### Scenario: The run is resumed
- **WHEN** the run that step blocked is resumed
- **THEN** no model call is made, and the run is `blocked` with `budget_exceeded` again

### Requirement: A call that fails at its limit is not tried again
When a model call fails and either the runner says it stopped the call at its limit, or the cost it reports is within a cent of what the call was allowed, the step SHALL fail with the error type `budget_exceeded`, SHALL NOT be tried again whatever its `retry` says, and the cost SHALL still be counted. A call that fails having spent less SHALL fail the attempt with `runner_failed`, which may be tried again.

#### Scenario: A call fails having spent everything
- **WHEN** a step with a budget of $5 and `retry: { attempts: 3 }` makes a call that fails having cost $5
- **THEN** the step fails with the error type `budget_exceeded` after that one call
- **AND** the run is `blocked` with `budget_exceeded`, and its `spent_usd` is 5

#### Scenario: The runner says it stopped at the limit
- **WHEN** the runner reports that it stopped the call at its limit, having cost $1 of the $5 allowed
- **THEN** the step fails with the error type `budget_exceeded` after that one call

#### Scenario: A failure with money left
- **WHEN** the same step's first call fails for another reason having cost $1
- **THEN** the step is tried again, and the second call is told to stop at $4

## MODIFIED Requirements

### Requirement: A step's budget is a limit on what its agent may spend
Each model call of a step SHALL be allowed the lesser of what the step's `budget.usd` has left and what the run has left, and the runner SHALL be told that figure as the most the call may spend. A step whose call cost more than it was allowed SHALL fail with the error type `budget_exceeded`, SHALL NOT be tried again, and its cost SHALL still be counted. `budget.minutes`, when set, SHALL limit how long the agent may run.

#### Scenario: The budget is passed on
- **WHEN** a step with `budget: { usd: 15 }` runs on Claude for the first time, in a run with no budget
- **THEN** Claude's command line is told to stop at 15.00 dollars

#### Scenario: The run has less left than the step
- **WHEN** a step with a budget of 5 dollars runs in a run that has 1 dollar left
- **THEN** the runner is told to stop at 1 dollar

#### Scenario: A call costs more than the step allows
- **WHEN** a call that was allowed 1 dollar comes back having cost 2 dollars
- **THEN** the step fails with the error type `budget_exceeded`
- **AND** 2 dollars is added to what the run has spent
