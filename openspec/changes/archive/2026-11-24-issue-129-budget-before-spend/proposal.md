## Why

A budget is a limit, not a report, and three things let money be spent past one. A run's budget was checked only between steps, so the step in flight could overspend it by as much as its own budget. A step's budget was given to the model afresh on every attempt, so a step with three attempts could spend three times its budget. And a call that the command line stopped at its limit was reported as `runner_failed` and tried again, which spent more. Closes issue 129.

## What Changes

- When a worker is handed a step, it is told what the step's earlier attempts spent and what the run has left. Both are read in the transaction that hands the step out.
- Each model call of an ai step is allowed the lesser of what the step's budget has left and what the run has left. That figure, not the step's whole budget, is what the runner is told to stop at.
- A step's budget covers all the attempts of one visit together. When less than a cent is left, no model call is made: the step fails with `budget_exceeded` and is not tried again.
- A call that fails having spent what it was allowed, or that the runner says it stopped at its limit, fails the step with `budget_exceeded` and is not tried again. It used to be `runner_failed`, and retried.
- The Claude adapter says, in a form the worker can tell from any other failure, when the command line reports `error_max_budget_usd`.
- A call that comes back having cost more than it was allowed fails its step with `budget_exceeded`, as before, but measured against what it was allowed and not the step's whole budget.

What is still not guaranteed:

- A runner with no limit of its own is only checked after the call. Codex is one, and so is the scripted runner used in tests. Such a call can cost more than it was allowed; the step then fails, and the money has gone.
- Claude's command line stops when it has passed its limit, not before, so a call can cost a little more than it was allowed.
- A `decide` step has no budget of its own and its call is not limited. It can still take a run past its budget; the run is blocked when the step finishes.
- The cost of an agent in a container of its own is the agent's word. The limit travels in its job and Claude's command line enforces it there, but what it reports back cannot be checked.
- Resuming a run does not give a step more money. A run that is blocked because a step spent its own budget stops again at once when resumed, without a call. The way on is a new version of the workflow with a larger budget, and a new run.
- What the run has left is read when the step is handed out. Runs do one step at a time, so nothing else can spend it meanwhile.
- That Claude's command line reports `error_max_budget_usd` is taken from its documentation and tested against a stand-in. It has not been seen from the real command line.

Rules that must not be weakened: this change makes "a budget is a limit, not a report" hold where it did not. "One transaction per transition" is kept: what a call may spend is read inside the transaction that claims the step, and the failure, its cost and its event still commit together.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `runs`: a model call may spend no more than its run has left; the budget is no longer only checked between steps.
- `ai-steps`: a step's budget covers all its attempts; a call that fails at its limit is `budget_exceeded` and is not tried again.
- `model-providers`: the Claude adapter is given the most a call may spend, and says when the command line stopped at it.
- `isolated-agents`: the job carries the most the call may spend, not the step's whole budget.

## Impact

- `engine/transitions.go`: `Work` carries what the step has spent and what the run has left, filled in by `Claim`.
- `worker/worker.go`: works out what a call may spend, refuses a call with nothing left, and tells a failure at the limit from any other.
- `runner/runner.go`, `runner/claude.go`: `ErrBudget`, and the scripted runner records what each call was allowed.
- No change to the database, the API or the workflow YAML.
- A workflow that relied on a retry after `runner_failed` at the limit, or on each attempt having the step's whole budget, will now stop with `budget_exceeded`.
