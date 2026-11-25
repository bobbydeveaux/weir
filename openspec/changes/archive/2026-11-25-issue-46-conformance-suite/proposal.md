## Why

The contract Weir offers is its event log: what a run records, in what order. Nothing states that log as data, so a change to the engine can alter it without anyone deciding to, and another implementation has nothing to check itself against. The first tagged version promises a conformance suite.

Closes issue 46.

## What Changes

- A conformance suite in `conformance/`: each case is a folder holding a definition, its inputs with what the world outside does, and the event log the run must produce.
- A test runs every case and fails on any difference in a run's status, the type of its error, or its event log.
- Eight first cases, covering the five step kinds, separation of duties, the default label, polling, a spent budget, output that fails its schema, events that start and resume, and cancelling.
- A way to record a case's log, for writing a new case.

Nothing in the engine changes.

Out of scope: comparing each event's actor and data; running the suite against a server over HTTP; cases for crashes, leases, compensation, hooks, schedules, files, tools and isolated agents.

## Capabilities

### New Capabilities

- `conformance`: what a conformance case is, what is checked, and that the suite passes.

### Modified Capabilities

None.

## Impact

- **Code:** `conformance/conformance_test.go` and the cases. No engine, API or database change.
- **Dependencies:** none new. `gopkg.in/yaml.v3` is already a direct dependency.
- **Rules that must not be weakened:** none is touched.
- **Not proven here:** that the suite can be run by anything but Weir's own tests.
