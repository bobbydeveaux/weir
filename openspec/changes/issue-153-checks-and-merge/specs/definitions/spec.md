## ADDED Requirements

### Requirement: A step that waits on checks must set a timeout
Publishing SHALL refuse a definition whose `github.checks` step sets no `timeout`, as it refuses a callback step without one. A pipeline that never finishes would otherwise hold the run for ever.

#### Scenario: No timeout
- **WHEN** a definition is published whose `github.checks` step has no `timeout`
- **THEN** publishing is refused, naming the step and `timeout`

#### Scenario: A timeout is set
- **WHEN** the same step sets `timeout: 2h`
- **THEN** publishing accepts it

### Requirement: Publishing warns of a loop with no limit
When a definition's `next` rules can send a run back to an earlier step, and no rule on that loop tests a `visit` number, publishing SHALL accept the definition with a warning that names the loop. The run's budget is then the only limit on the loop.

#### Scenario: A loop with a limit
- **WHEN** `check` leads to `fix` while `steps.check.visit < 3`, and `fix` leads back to `check`
- **THEN** publishing accepts the definition with no warning

#### Scenario: A loop without one
- **WHEN** `check` leads to `fix` on `state == "failed"`, and `fix` leads back to `check`
- **THEN** publishing accepts the definition and warns that the loop `check → fix → check` has no visit limit
