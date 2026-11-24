# sampling Specification

## Purpose
Sampling lets a workflow send a fraction of its runs to a person for review and let the rest go on. The pick is worked out from a seed alone, so it is the same every time and can be checked afterwards.

## Requirements

### Requirement: A sample picks about the stated fraction
The expression `sample(seed, rate)` SHALL take a seed, which is text, and a rate, which is a number, and be true for about `rate` of all seeds.

#### Scenario: One run in ten
- **WHEN** `sample(run.id + "/approve", 0.1)` is worked out for 4,000 different runs
- **THEN** it is true for about 400 of them

### Requirement: The same seed always gets the same answer
The pick MUST depend on the seed and the rate alone. There is no chance in it: the same seed at the same rate SHALL get the same answer every time it is asked, on any server.

#### Scenario: A run is asked about twice
- **WHEN** `sample(run.id + "/approve", 0.1)` is worked out again for a run that was picked
- **THEN** the run is picked again
- **AND** a run that was not picked is not picked the second time either

### Requirement: Each seed picks its own set of runs
Two samples with different seeds SHALL pick sets of runs that are independent of each other, so that a workflow which samples at two steps can give each its own seed and not send the same runs to both.

#### Scenario: Two steps sample the same runs
- **WHEN** one step uses the seed `run.id + "/approve"` and another `run.id + "/sign"`, both at a rate of 0.1
- **THEN** most of the runs picked at one step are not picked at the other

### Requirement: A rate of 0 picks nothing and a rate of 1 picks everything
A rate of 0 SHALL be false for every seed, and a rate of 1 SHALL be true for every seed. Either may be written as a whole number.

#### Scenario: A rate of 0
- **WHEN** `sample(run.id, 0)` is worked out for any run
- **THEN** it is false

#### Scenario: A rate of 1
- **WHEN** `sample(run.id, 1)` is worked out for any run
- **THEN** it is true

### Requirement: A rate outside 0 to 1 is an error when the rule runs
A rate below 0 or above 1 MUST be an error, with a message that says the rate must be between 0 and 1. The error is raised when the expression is worked out, not when the workflow is published. A `next` rule that fails this way SHALL fail the run with the error type `expression_failed`.

#### Scenario: A rate of 1.5
- **WHEN** `sample(run.id, 1.5)` is worked out
- **THEN** it is an error that says the rate must be between 0 and 1

#### Scenario: A next rule with a bad rate
- **WHEN** a step finishes, and its `next` rule calls `sample` with a rate of 2
- **THEN** the run fails with the error type `expression_failed`

### Requirement: A sample with the wrong kinds of argument is refused when published
An expression that gives `sample` anything but text for the seed and a number for the rate SHALL fail its check, so that a workflow which holds it in a `next` rule cannot be published.

#### Scenario: The arguments the wrong way round
- **WHEN** the expression `sample(0.1, run.id)` is checked
- **THEN** it is refused

### Requirement: A next rule can send a sample of runs to a person
A `next` rule SHALL be able to use `sample` with any other condition, so that a workflow can let a run go on when it is inside a limit and not picked, and send every other run to a human step: the exceptions, and the sample. Weir does not record on the run whether it went to a person as a sample or as an exception.

#### Scenario: Inside the limit and not picked
- **WHEN** a step's first rule is `when: output.within_limits && !sample(run.id + "/approve", 0.1)` going to `apply`, its second rule goes to the human step `approve`, and a run inside the limit is not picked
- **THEN** the run goes to `apply`

#### Scenario: Inside the limit and picked
- **WHEN** a run inside the limit is one of those picked
- **THEN** the run waits at `approve` for a person

#### Scenario: Outside the limit
- **WHEN** a run is outside the limit
- **THEN** the run waits at `approve`, whether or not it would have been picked
