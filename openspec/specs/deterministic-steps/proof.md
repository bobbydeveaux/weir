# Proof: deterministic-steps

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A deterministic step runs a registered handler | `engine/engine_test.go:TestCancelUndoesCompletedStepsInReverse`, `engine/engine_test.go:TestARunThroughAllFiveKinds` |
| A handler is given the step's input, the run and the step | `engine/engine_test.go:TestCancelUndoesCompletedStepsInReverse` (the step and the input are asserted; the run's id is not) |
| A handler is given the same idempotency key on every attempt | Not tested: the key is set when a step is claimed, in `engine/transitions.go` (`Claim`), and no test reads it in a handler |
| Publishing refuses a handler that is not registered | `engine/engine_test.go:TestPublishRefusesWhatCannotRun`, `spec/definition_test.go:TestValidateChecksHandlersWhenAsked` (a handler that is not registered is asserted; a step that names none is not) |
| A handler's error is retried unless it is marked permanent | `engine/engine_test.go:TestRetriesThenBlocksThenResumes`, `engine/engine_test.go:TestCancelUndoesCompletedStepsInReverse` (retrying, and the `handler_failed` type of a permanent error, are asserted; no test gives a permanent error attempts to spare and checks they are not used) |
| A worker without the handler fails the step | Not tested: the failure is in `worker/worker.go` (`deterministic`), and no test runs a step on a worker that lacks its handler |
| Two handlers are built in | `engine/engine_test.go:TestRetriesThenBlocksThenResumes`, `api/events_test.go:TestAnEventStartsEveryWorkflowWaitingForIt` (a `core.noop` step is asserted to succeed; `core.echo` is only published, and what it returns is not asserted) |
| A handler's output is checked against the step's schema | Not tested: for a deterministic step. The check is in `engine/transitions.go` (`completeTx`) and is the same for every kind; `engine/engine_test.go:TestOutputThatFailsItsSchemaIsRejected` proves it for an ai step only |
| A deterministic step takes no field of another kind | `spec/definition_test.go:TestValidateFindsEveryProblem` |
| The server lists its registered handlers | `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead` (that at least two handlers are listed is asserted; their names are not) |
