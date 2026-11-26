## Context

`Worker.Run` claims a step, runs it, and claims the next. The claim itself (`Engine.Claim`) is already safe for many callers: it takes the oldest runnable step with `FOR UPDATE SKIP LOCKED` in one short transaction, and a unique index allows one live attempt per step. Several servers on one database are already supported. So running several steps in one process needs no change to the engine; it is a change to the worker's loop.

## Decisions

**Slots, then claim.** The loop takes a slot before it claims, and gives the slot back if there was nothing to claim. A worker never holds an attempt it has no slot for, so a claimed step never sits waiting inside one process while another server is idle. The alternative, N independent loops each calling `RunOnce`, would also work, but would sweep N times as often and make the first error harder to handle.

**One loop sweeps.** The loop sweeps about once a second. While every slot is busy it wakes once a second to sweep, instead of blocking until a step finishes. Sweeps from several places were already safe.

**Heartbeats stay per step.** `RunOnce` already starts a heartbeat for the step it holds. Splitting it into `claim` and `run` keeps that unchanged, and each concurrent step has its own heartbeat and its own cancellation.

**An error stops claiming, and lets the others finish.** Today an error from a step (a failed write to the database, not a failed step) ends `Run`. With several steps in flight, the worker stops claiming and waits for the steps it holds, without cancelling them, then returns the first error. Cancelling them would throw away model calls already paid for; their leases would run out and they would be run again. A step that is shut down by the context being cancelled is treated as today.

**A ceiling of 256.** A typo such as 10000 would otherwise open that many agent containers. 256 is far above what one process's database pool can feed.

## Risks

- The database pool. `pgxpool` defaults to the larger of 4 and the number of CPUs. Each step holds a connection only for a short write, so more steps than connections wait rather than fail, but a high setting on a small pod should raise `pool_max_conns` in the database address. This is documented, not enforced.
- Runners and launchers must be safe to call at once. The built-in ones hold no state between calls: the command-line runners make a directory per call, the Kubernetes launcher names each Job from its token, and the Cloud Run launcher locks its own state. The fake runner locks.
