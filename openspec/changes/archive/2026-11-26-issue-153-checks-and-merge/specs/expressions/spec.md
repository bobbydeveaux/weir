## MODIFIED Requirements

### Requirement: An expression sees the run's input, its earlier steps and the run
Inside a run, an expression SHALL see `input`, the run's input; `steps`, the steps the run has reached, by id; and `run`, with `run.id` and `run.requester`. Each step under `steps` SHALL have `output`, `label` for a decide step, `outcome` for a human step, `files`, and `visit`, the number of its latest visit. When a step has been visited more than once, `steps.<id>` SHALL be its latest visit.

#### Scenario: Reading an earlier step's output
- **WHEN** the step `open` finished with the output `{"number": 482}`, and a later mapping holds `steps.open.output.number`
- **THEN** the mapping gives 482

#### Scenario: Falling back when a field is absent
- **WHEN** an expression tests `has(steps.approve.output.data.terms)` and the decision carried no data
- **THEN** the expression takes its other branch and does not fail

#### Scenario: A rule stops a loop
- **WHEN** the step `check` has the rules `{ when: steps.check.visit < 3, go: fix }` then `{ go: stop }`, and `fix` leads back to `check`
- **THEN** the run goes through `check` three times and `fix` twice, and then reaches `stop`
