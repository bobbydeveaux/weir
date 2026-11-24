# Proof: failure-handling

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A failed attempt is retried while attempts remain | `engine/engine_test.go:TestRetriesThenBlocksThenResumes`, `api/shell_test.go:TestAStepsShellReachesItsAgentAndARefusalIsNotRetried` |
| Each retry waits longer than the last | Not tested: the wait is worked out in `failTx` in `engine/transitions.go`, and the tests set it to a millisecond and do not measure it |
| A step that fails for good blocks the run by default | `engine/engine_test.go:TestRetriesThenBlocksThenResumes`, `engine/engine_test.go:TestOutputThatFailsItsSchemaIsRejected` |
| A step with on_failure continue is passed over | `engine/engine_test.go:TestContinueCarriesOnPastAFailedStep` |
| A step with on_failure cancel undoes finished steps in reverse | `engine/engine_test.go:TestCancelUndoesCompletedStepsInReverse` (the order, the statuses and the run's error are asserted; publishing refusing `compensate` on another kind of step is not) |
| An undo handler that fails ends the run | Not tested: the rule is in `stepFailed` in `engine/transitions.go`, and no test makes an undo handler fail |
| A blocked run can be resumed from where it stopped | `engine/engine_test.go:TestRetriesThenBlocksThenResumes` (the engine is asserted; the 409 over the API is not) |
| A run that spent its budget is resumed with a larger one | `engine/engine_test.go:TestARunStopsWhenItsBudgetIsSpent` |
| A run that has not finished can be cancelled | `api/hooks_test.go:TestABlockedRunCanBeCancelledAndIsThenCountedAsFailed` (the status, the 403 and the refused resume are asserted; stale tokens, closed tasks and steps left as they were are not) |
| Output that fails its JSON Schema fails the attempt | `engine/engine_test.go:TestOutputThatFailsItsSchemaIsRejected` (an agent's output is asserted; a callback's output and the retry are not) |
