## MODIFIED Requirements

### Requirement: A step's budget is a limit on what its agent may spend
The step's `budget.usd` SHALL be given to Claude's command line as the most the call may spend. Codex's command line takes no such limit, so a Codex call runs to its end. A step whose call cost more than its budget SHALL fail with the error type `budget_exceeded`, SHALL NOT be tried again, and its cost SHALL still be counted. `budget.minutes`, when set, SHALL limit how long the agent may run.

#### Scenario: The budget is passed on
- **WHEN** a step with `budget: { usd: 15 }` runs on Claude
- **THEN** Claude's command line is told to stop at 15.00 dollars

#### Scenario: A call costs more than the step allows
- **WHEN** a call for a step with a budget of 1 dollar comes back having cost 2 dollars
- **THEN** the step fails with the error type `budget_exceeded`
- **AND** 2 dollars is added to what the run has spent

#### Scenario: A Codex step costs more than it allows
- **WHEN** a Codex step with a budget of 1 dollar and `retry: { attempts: 3 }` ends with an estimated cost of 2 dollars
- **THEN** the step fails with the error type `budget_exceeded`
- **AND** the Codex command line is not run a second time
