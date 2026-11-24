## Why

An external step with `resume: poll` may set a `timeout`, and nothing acts on it. A timeout is a timer, and a timer is set only when a step parks to wait for a callback. A poll step never parks: each poll that is not yet done puts the step back in the queue. So a poll whose condition never holds asks the third party for ever, and the run never ends.

Closes issue 127.

## What Changes

- When a poll comes back with its condition false, the engine checks the step's `timeout`. If that long has passed since the step began polling (the first poll since it last failed), its attempt is closed as `timed_out` and the step fails with the error type `timeout`, as a callback step does when its timer fires.
- The step's `on_failure` then decides what happens to the run. By default the run is `blocked`.
- The check is made only when a poll comes back, so a step can run over its timeout by up to one `poll.every`.
- A poll step is still not required to set a timeout. One that sets none polls until its condition holds, as before.

Out of scope:

- Requiring a timeout on a poll step. Published definitions without one must keep working.
- A timer that stops a poll step between polls.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `external-steps`: a poll step's timeout is enforced, and the requirement that a poll repeats says where it stops.

## Impact

- `engine/transitions.go`: `Defer` checks the timeout before it queues the step again, in the same transaction. This keeps the rule "one transaction per transition": the attempt, the step, the run and their events commit together.
- No change to the database, the API or the YAML.
- A published definition whose poll step sets a `timeout` now stops at it. Before, the timeout was ignored.
- `README.md`, `docs/build/02-epics.md`, and `openspec/specs/external-steps/`.
