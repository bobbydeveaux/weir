# expressions Specification

## Purpose
Expressions are how a definition reads data and makes choices: which step comes next, what a step is given, whether an event starts a run, and when polling stops. They are written in CEL.

## Requirements

### Requirement: Expressions are CEL and are checked when a definition is published
Every expression in a definition SHALL be written in CEL. Publishing MUST refuse a definition with an expression that does not compile, and the error SHALL name where it is: the step and the rule, mapping or condition. This covers `next[].when`, every string in a step's `input` and in `call.body`, `poll.until`, and a trigger's `when` and `input`.

#### Scenario: A rule that is cut short
- **WHEN** a `next` rule's `when` is `label ==`
- **THEN** publishing is refused, and the error names `next[0].when`

#### Scenario: A function given the wrong kinds of value
- **WHEN** an expression calls `sample(0.1, run.id)`, with the number and the text the wrong way round
- **THEN** the expression is refused when it is checked, not when a run reaches it

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

### Requirement: A next rule also sees what the step has just produced
A `next` rule's `when` SHALL also see `output`, the output of the step that has just finished; `label`, the label a decide step chose; and `outcome`, the outcome a person chose. A step's `input` mapping is resolved before the step runs, and sees `input`, `steps` and `run` only.

#### Scenario: Routing on a label
- **WHEN** a decide step chooses `fix`, and its first rule is `when: label == "fix"`
- **THEN** that rule matches

#### Scenario: Routing on an outcome
- **WHEN** a person rejects a task, and the step has the rule `when: outcome == "rejected"`
- **THEN** that rule matches

### Requirement: A condition must be true or false
A `when` or an `until` MUST give true or false. When a `next` rule's `when` gives anything else, or cannot be worked out, the engine SHALL end the run as `failed` with the error type `expression_failed`. It MUST NOT guess which way to go.

#### Scenario: A rule that gives text
- **WHEN** a `next` rule's `when` is `input.title` and the title is a string
- **THEN** the run is `failed` with the error type `expression_failed`

### Requirement: An input mapping is resolved before its step starts
A step's `input` is a mapping. The engine SHALL treat every string in it as an expression and replace it with its value. Numbers, booleans, lists and nested mappings SHALL be kept as written, with the strings inside them evaluated in turn. A fixed string MUST be quoted inside the expression, as `'"hello"'`.

#### Scenario: Strings are evaluated and the rest is kept
- **WHEN** a mapping holds `title: input.title`, `fixed: '"always this"'`, `count: 3` and `labels: ['"remediation"']`
- **THEN** `title` is the run's title, `fixed` is `always this`, `count` is 3 and `labels` is a list holding `remediation`

#### Scenario: A mapping that cannot be resolved
- **WHEN** a run reaches a step whose mapping holds an expression that fails
- **THEN** the run is `failed` with the error type `input_invalid`

### Requirement: A step with no mapping is given a default input
A step that sets no `input` mapping SHALL be given the run's input if it is a `decide` or an `ai` step, and `{}` if it is any other kind.

#### Scenario: A model step given nothing
- **WHEN** a decide step has no `input`, and the run's input is `{"title": "Checkout is down"}`
- **THEN** the step's input is `{"title": "Checkout is down"}`

#### Scenario: An external step given nothing
- **WHEN** an external step has neither `input` nor `call.body`
- **THEN** it sends `{}`

### Requirement: A trigger's expressions see the event
A trigger's `when` and its `input` mapping SHALL see `event`, with `event.type`, `event.id`, `event.source` and `event.data`. They see nothing of any run, because no run exists yet.

#### Scenario: Only PDFs start a run
- **WHEN** a trigger has `when: event.data.type == "pdf"` and `input: { document: event.data.name }`, and an event arrives with the type `pdf` and the name `treaty.pdf`
- **THEN** a run starts with the input `{"document": "treaty.pdf"}`

### Requirement: A poll condition sees the reply
An external step's `poll.until` SHALL see `response`, the JSON body of the reply to the call it has just made, and nothing else.

#### Scenario: Polling until done
- **WHEN** a step has `until: response.status == "done"`, and the third reply is the first with that status
- **THEN** the step finishes on the third call, with that reply as its output

### Requirement: A duration is a number and a unit
A duration in a definition (`timeout`, `retry.backoff`, `poll.every`, `escalate[].after`) SHALL be written as a number and a unit, such as `30s`, `5m`, `48h` or `2d`, where `d` is a day of 24 hours. Publishing MUST refuse anything else, and a negative duration.

#### Scenario: Two days
- **WHEN** a timeout is written `2d`
- **THEN** it is 48 hours

#### Scenario: A word
- **WHEN** a timeout is written `soon`
- **THEN** it is refused as not a duration

### Requirement: The sample function picks a fixed fraction of seeds
An expression MAY call `sample(seed, rate)`, with a text seed and a rate from 0 to 1. It SHALL be true for about that fraction of all seeds, and SHALL always give the same answer for the same seed. A rate of 0 is never true and a rate of 1 is always true. A rate outside 0 to 1 SHALL be an error when the expression is evaluated.

#### Scenario: The same answer every time
- **WHEN** `sample(run.id + "/approve", 0.1)` is evaluated twice for the same run
- **THEN** it gives the same answer both times

#### Scenario: A rate above one
- **WHEN** `sample(run.id, 1.5)` is evaluated
- **THEN** it fails with an error saying the rate must be between 0 and 1
