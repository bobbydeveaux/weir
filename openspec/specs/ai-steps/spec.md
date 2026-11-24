# ai-steps Specification

## Purpose
An ai step gives an agent a piece of work: a pinned prompt, a budget it cannot exceed, and a schema its answer must match. The step says which documents the agent may read and whether it may write files, and Weir keeps what it wrote for later steps.

## Requirements

### Requirement: An ai step runs a pinned prompt template
An ai step SHALL name its `prompt` as a template at a pinned version, such as `fix-incident@7`. Publishing SHALL refuse a prompt that is not pinned, or a version that does not exist in the workspace. When the step runs, `{{name}}` in the template SHALL be replaced by that value from the step's input, and `{{input}}` by the whole input as JSON. A step with no `input` mapping SHALL be given the run's input.

#### Scenario: The input reaches the prompt
- **WHEN** a step with `input: { changed: steps.fix.files }` runs a template whose user prompt is `{{input}}`
- **THEN** the agent's prompt holds that input as JSON

#### Scenario: A prompt with no version
- **WHEN** a definition whose ai step has `prompt: fix-incident` is published
- **THEN** publishing is refused, and the error says the prompt must name a pinned version

### Requirement: An ai step must declare an output schema and a budget
Publishing SHALL refuse an ai step that has no `output.schema`, or no `budget.usd` above zero.

#### Scenario: Neither is declared
- **WHEN** a definition whose ai step has only a prompt is published
- **THEN** publishing is refused
- **AND** the error names both the output schema and the budget

### Requirement: The engine checks an agent's output against the schema
The step's schema SHALL be passed to the model as the shape its answer must take, and the engine SHALL check the answer against the schema itself, whatever the model or the worker claims. An answer that does not match SHALL fail the attempt with the error type `output_invalid`. An agent that returns no structured answer at all SHALL fail the attempt.

#### Scenario: The answer lacks a required field
- **WHEN** an agent returns `{"wrong": 1}` for a step whose schema requires `branch`
- **THEN** the step fails with the error type `output_invalid`
- **AND** with the default failure policy the run is `blocked`

### Requirement: A step's budget is a limit on what its agent may spend
Each model call of a step SHALL be allowed the lesser of what the step's `budget.usd` has left and what the run has left, and the runner SHALL be told that figure as the most the call may spend. Codex's command line takes no such limit, so a Codex call runs to its end. A step whose call cost more than it was allowed SHALL fail with `budget_exceeded`, SHALL NOT be tried again, and its cost SHALL still be counted. `budget.minutes`, when set, SHALL limit how long the agent may run.

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

#### Scenario: A Codex step costs more than it allows
- **WHEN** a Codex step with a budget of 1 dollar and `retry: { attempts: 3 }` ends with an estimated cost of 2 dollars
- **THEN** the step fails with the error type `budget_exceeded`
- **AND** the Codex command line is not run a second time

### Requirement: Every model call is recorded with its cost
Each attempt at an ai step that calls a model SHALL record the call: the runner, the provider, the model, the template and its version, a hash of the prompt, the prompt as sent and the final reply, the tokens in and out, the cost, whether the cost is estimated, and how long the call took. The cost SHALL be added to what the step and the run have spent, whether the attempt succeeded or failed.

#### Scenario: A finished run shows what it cost
- **WHEN** a run's only model call cost 3.20 dollars
- **THEN** the run has spent 3.20 dollars
- **AND** the run's record holds the call, with its prompt and its cost

### Requirement: An agent may read only the documents its step lists
An ai step MAY list `files`: documents fetched for its agent before it starts. An entry SHALL name either an earlier step, for every file that step wrote, or a `connection`, a `path` and the `name` the agent sees. The agent SHALL be able to read those files and no others. A document that cannot be fetched, or is larger than 20 MB, SHALL fail the attempt with the error type `file_unavailable`.

#### Scenario: One document is listed
- **WHEN** a step lists `treaty.pdf` and its agent runs
- **THEN** the file is in the agent's working directory
- **AND** the agent is allowed to read that directory and nothing else

#### Scenario: A name that leaves the directory
- **WHEN** an agent is to be given a file named `../escape.txt`
- **THEN** the attempt fails
- **AND** the agent is not started

### Requirement: Files an agent writes are kept for later steps
An ai step with `writes:` SHALL let its agent write files, in one directory named `out` and nowhere else. When the step succeeds, Weir SHALL keep every file written there. A later step SHALL see them in expressions as `steps.<id>.files`, a list of `path`, `size`, `sha256` and `content_type` with no contents, and an ai step that lists `files: [{step: <id>}]` SHALL be given the files themselves under the paths they were written at. A file written outside `out` SHALL NOT be kept.

#### Scenario: A draft is handed to a reviewer
- **WHEN** the step `draft` writes `report.md`, and the step `review` lists `files: [{step: draft}]`
- **THEN** the review agent is given `report.md` with the same contents
- **AND** `steps.draft.files` holds its path and size

#### Scenario: A file written elsewhere
- **WHEN** an agent writes one file inside `out` and one beside it
- **THEN** only the file inside `out` is kept

### Requirement: What an agent writes is limited
A step's `writes` SHALL limit the number of files and their total size. With nothing set the limits are 20 files and 8 MB, and publishing SHALL refuse more than 200 files or 64 MB. Going over a limit, or writing anything that is not an ordinary file, SHALL fail the attempt, and nothing the agent wrote SHALL be kept. Publishing SHALL refuse a `files` entry that names the step itself, or a step that has no `writes`.

#### Scenario: Too many files
- **WHEN** an agent writes two files and its step allows one
- **THEN** the attempt fails, saying the agent wrote more than 1 file

#### Scenario: Reading from a step that writes nothing
- **WHEN** a definition lists `files: [{step: draft}]` and `draft` has no `writes`
- **THEN** publishing is refused

### Requirement: The step's tier and the template's model and effort are acted on
A step's `tier` SHALL choose the model, by the server's map of tier names to models. A step that names no tier SHALL use the `model` its template names, or the command line's own default. A template's `effort` SHALL be passed to the model, and a template whose effort is not a known value SHALL be refused.

#### Scenario: A template asks for more thought
- **WHEN** a step's template sets `effort: high`
- **THEN** Claude's command line is run with that effort

#### Scenario: An effort nobody knows
- **WHEN** a template is published with an effort other than `low`, `medium`, `high`, `xhigh` or `max`
- **THEN** the template is refused

#### Scenario: A step for Codex
- **WHEN** a step with `tier: codex` runs with its agent beside the worker
- **THEN** the step is sent to Codex

### Requirement: Publishing names the parts of a definition that are not acted on
A definition's `tiers` block, `minutes` in its top-level `budget`, a step's `sandbox`, and tools listed under `tools.approve` are not acted on. Publishing SHALL accept them and SHALL list each one in its reply as not yet enforced, so that a publisher is never led to believe they are in force.

#### Scenario: A definition with a tiers block
- **WHEN** a definition with a top-level `tiers` block is published
- **THEN** it is published
- **AND** the reply lists `tiers` under `not_yet_enforced`

#### Scenario: A definition with a time limit on its runs
- **WHEN** a definition with a top-level `budget` that sets `minutes: 30` is published
- **THEN** it is published
- **AND** the reply lists `budget.minutes` under `not_yet_enforced`

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
