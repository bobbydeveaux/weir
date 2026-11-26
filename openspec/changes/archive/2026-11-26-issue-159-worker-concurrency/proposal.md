## Why

A Weir server's worker runs one step at a time: `Worker.Run` loops on `RunOnce`, which claims a step and runs it to the end before claiming the next. When many runs start together, their AI steps run strictly one after another, although the worker spends nearly all of that time waiting on a model. Thirty runs whose AI step takes about 17 seconds take about 8.5 minutes. Short steps queue behind long ones, so a run's last `core.noop` waits behind every AI step claimed before it, and runs finish together in batches. The sweep also runs only between steps, so a server busy with one long step acts on no expired lease or due timer until it finishes.

More servers help, but each costs a process, a database pool and a pod, for work that is mostly waiting.

Closes issue 159.

## What Changes

- A new setting, `WEIR_WORKER_CONCURRENCY`: how many steps one worker runs at once. The default is 1. Anything that is not a whole number from 1 to 256 is refused at start, naming the setting.
- The worker claims a step only when it has a free slot, so it never holds a step it cannot start. Each step is still claimed by exactly one worker, through the existing claim.
- Each running step keeps its own lease alive with its own heartbeats.
- The sweep runs about once a second whether or not every slot is busy. This is a change even at the default of 1: the sweep no longer waits for the running step to finish.
- When a step ends with an error the worker cannot recover from, the worker stops claiming, lets the steps it already holds finish, and returns the first error.
- The Helm chart gets `worker.concurrency`, default 1, which sets `WEIR_WORKER_CONCURRENCY`.

Out of scope: waking the worker on a Postgres notification (issue 10); limits per kind of step or per workspace; sizing the database pool to the setting (the README says when to raise `pool_max_conns`).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `workers-and-tokens`: a worker runs up to the configured number of steps at once; heartbeats are per step; the sweep runs while steps are running.
- `deployment`: the setting and its refusal, and the chart value.

## Impact

- **Code:** `worker/worker.go` (`Run`, and `RunOnce` split into claiming and running), `cmd/weir` (reading the setting), the Helm chart.
- **Dependencies:** none.
- **Rules that must not be weakened:** "one transaction per transition" is untouched: each claim, completion and failure is still its own transaction. Budgets are unchanged: a step's allowance is worked out from the database when it is claimed, as now. Two steps of one run with a run budget can now be in flight at once from one server, which was already possible with two servers.
- **Who is affected:** nobody, unless they set the new value. At the default the only difference is that the sweep keeps running during a long step.
