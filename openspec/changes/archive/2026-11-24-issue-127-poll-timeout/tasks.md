## 1. The engine

- [x] 1.1 In `Engine.Defer`, when the step sets a `timeout` and that long has passed since the step row was created, do not queue the step again
- [x] 1.2 Close the attempt as `timed_out` and fail the step with the error type `timeout`, through the same path a timed-out callback takes, in the one transaction

## 2. Tests

- [x] 2.1 A poll step whose third party never finishes: the run is `blocked` with the error type `timeout`, and the third party is not called again
- [x] 2.2 A poll step with a generous timeout that finishes in time is not stopped
- [x] 2.3 The new test fails without the change

## 3. Finish

- [x] 3.1 Update `openspec/specs/external-steps/proof.md`
- [x] 3.2 Update `docs/build/02-epics.md` and `README.md`
- [x] 3.3 Run `make check` and `make spec`
