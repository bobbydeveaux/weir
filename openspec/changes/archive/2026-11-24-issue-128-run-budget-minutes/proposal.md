## Why

A definition's top-level `budget` accepts `minutes` as well as `usd`. Nothing reads `minutes` there: a run has no time limit. Publishing already names the other parts of a definition that parse and are not acted on (`tiers`, a step's `hooks`, a step's `sandbox`, tools under `tools.approve`), but not this one. A publisher who writes `budget: { usd: 5, minutes: 30 }` is led to believe a run stops after thirty minutes, and it does not.

Closes issue 128.

## What Changes

- Publishing lists `budget.minutes` as not yet enforced when a definition's top-level budget sets `minutes` above zero. It reaches a publisher the same way as the others: `not_yet_enforced` in the reply to `POST /v1/definitions`, and the note `weir publish` prints.
- A step's own `budget.minutes` is not listed. It is enforced: the worker stops an agent that runs longer.
- The console's page for a workflow shows a run's `budget.minutes` under "Accepted, not yet enforced" and no longer shows it beside the budget in dollars as though it were a limit. Triggers and hooks, which are acted on, move out from under that heading.
- The specs say that a run has no time limit.

Out of scope: enforcing a time limit on a run. That needs a decision first, on whether time spent waiting for a person or a third party counts, and it is a separate piece of work. This change makes no rule weaker: "a budget is a limit, not a report" holds for dollars as before, and Weir now says plainly that minutes on a run are not a limit.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `definitions`: "Publishing says what it accepts but does not enforce" now includes a definition's top-level `budget.minutes`, and says that a step's is not listed.
- `ai-steps`: "Publishing names the parts of a definition that are not acted on" now includes a definition's top-level `budget.minutes`.
- `runs`: "A run's budget is a limit on what it spends" now says that a run has no time limit.

## Impact

- `spec/definition.go`: `Unsupported` reports `budget.minutes`. `engine.Publish`, `api/api.go` and `cmd/weir/main.go` pass the list on unchanged.
- `console/src/routes/workflows/[namespace]/[name]/+page.svelte`.
- `README.md`, `docs/build/02-epics.md`.
- No change to the database or to how a run behaves. A definition that sets `budget.minutes` still publishes; the reply now names it.
