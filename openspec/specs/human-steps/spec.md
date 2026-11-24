# human-steps Specification

## Purpose
A human step stops a run until a person decides. It says who may decide, which outcomes they may choose, and what happens when nobody does.

## Requirements

### Requirement: A human step waits in an inbox
When a run reaches a human step, the engine SHALL open a task for it and set the run to `waiting`. The list of tasks SHALL show, for each one, the run, the step, the role or user it is assigned to, the outcomes on offer and what the step was given as input.

#### Scenario: A run reaches an approval
- **WHEN** a run reaches a human step assigned to the role `approver`
- **THEN** the run's status is `waiting`
- **AND** the list of tasks holds one task for that step, with the role `approver`

### Requirement: A decision is one of the step's outcomes
A decision MUST name one of the step's outcomes. A step that lists no outcomes SHALL offer `approved` and `rejected`. The engine SHALL refuse any other outcome and leave the task open.

#### Scenario: An outcome that is not on the list
- **WHEN** a person decides a task with the outcome `maybe`, and the step offers `approved` and `rejected`
- **THEN** the decision is refused
- **AND** the run is still `waiting`

### Requirement: The requester cannot decide their own run
When a step sets `assign.not: run.requester`, the engine SHALL refuse a decision from the person who started the run.

#### Scenario: The requester tries to approve
- **WHEN** alice starts a run and then tries to approve its task
- **THEN** the decision is refused as not allowed
- **AND** over the API the reply is 403

#### Scenario: Someone else approves
- **WHEN** bob decides the same task
- **THEN** the decision is accepted and the run moves on

### Requirement: A decision routes the run
The outcome SHALL be available to the step's `next` rules as `outcome`, so that a run can go one way when approved and another when rejected. The step's output SHALL record the outcome, who decided, their note, and any data they sent with the decision.

#### Scenario: A rejection goes another way
- **WHEN** a task is rejected, and the step has a rule `when: outcome == "rejected"` that goes to `needs_human`
- **THEN** the run waits at `needs_human`

### Requirement: A task is decided once
The engine SHALL refuse a decision on a task that has already been decided or has expired.

#### Scenario: Deciding twice
- **WHEN** a person decides a task that was decided a moment ago
- **THEN** the decision is refused because the task is closed

### Requirement: An undecided step escalates
A step MAY list escalations, each with a delay and a role. When a delay passes with the task still open, the engine SHALL reassign the task to that role and record the escalation level. A step with escalations MUST set a timeout.

#### Scenario: Nobody decides in time
- **WHEN** a task assigned to `approver` is not decided within the first escalation's delay
- **THEN** the task is assigned to the escalation's role, at level 1

### Requirement: An undecided step times out
When a human step's timeout passes with the task still open, the engine SHALL fail the step with the error type `timeout` and remove the task from the inbox. The step's failure policy then decides what happens to the run.

#### Scenario: The timeout passes
- **WHEN** a human step with the default failure policy is not decided before its timeout
- **THEN** the run is `blocked` with the error type `timeout`
- **AND** the task is no longer in the inbox

### Requirement: A human step says who decides
Publishing SHALL refuse a human step that assigns neither a role nor a user, and one whose `assign.not` is anything but `run.requester`.

#### Scenario: Nobody is assigned
- **WHEN** a definition with a human step that has no `assign` is published
- **THEN** publishing is refused, and the error names the step
