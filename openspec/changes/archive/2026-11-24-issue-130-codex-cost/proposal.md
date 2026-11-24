## Why

A Codex step always costs nothing, so a budget cannot stop it. Codex reports tokens and no cost. The adapter works a cost out from prices, and the server gave it none, so every Codex call was recorded at zero dollars. A step's budget was never reached, and the run's recorded spend was wrong. That breaks the rule "A budget is a limit, not a report".

Closes issue 130.

## What Changes

- Two new settings give Codex its prices: `WEIR_CODEX_USD_PER_MTOK_IN` and `WEIR_CODEX_USD_PER_MTOK_OUT`, in US dollars for a million tokens. A value that is not a number, or is less than zero, stops the server from starting, with an error that names the setting.
- With both prices set, a Codex call's cost is estimated from its tokens, as the adapter already could, and marked as estimated. The worker's existing check then fails a step whose estimate passes its budget, with `budget_exceeded`.
- Without both prices, the Codex adapter refuses a call that carries a budget, before the command line is started. The step fails with the new error type `budget_unenforceable`, is not tried again, and the message names the two settings. This keeps the rule "A provider that cannot give a guarantee refuses the step".
- **BREAKING**: an `ai` step must have a budget, so a server with no Codex prices no longer runs `tier: codex` steps at all. Before, it ran them and recorded a cost of zero.
- A Codex call with no budget, such as a `decide` step, still runs. Without prices it is recorded at zero dollars.

What is still not guaranteed:

- A Codex call cannot be stopped part-way. The Codex command line has no spending cap, so a call runs to its end and can cost more than the step's budget before the step fails.
- The cost is an estimate. It is only as right as the two prices, it uses one pair of prices whatever the model, and it charges cached input tokens at the full input price.
- A `decide` step on Codex with no prices set adds nothing to its run's spend, so the run's budget does not count it.
- An agent run in a container of its own is always run by Claude. `tier: codex` is ignored there, as before; this change does not touch that.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `model-providers`: Codex's prices become settings; a Codex call with a budget is refused when there are no prices; the requirement that said a server has no setting for prices is corrected.
- `ai-steps`: the requirement on a step's budget says which command line is given the budget as a cap, and that a Codex step is checked only after its call ends.

## Impact

- `internal/config`: reads and checks the two settings.
- `runner/codex.go`: the refusal, and a new error, `runner.ErrNoBudget`.
- `worker/worker.go`: fails a refused step with `budget_unenforceable`, without retrying.
- `cmd/weir/main.go`: passes the prices to the adapter.
- `README.md`, `docs/build/03-agent-runners.md`, `docs/build/02-epics.md`.
- Anyone running `tier: codex` steps must set the two prices when they upgrade.
