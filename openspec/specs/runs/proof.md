# Proof: runs

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A run starts on the live version, with an input and a requester | `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP`, `engine/engine_test.go:TestARunThroughAllFiveKinds`, `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead` (the start, the first event and the other workspace's refusal are asserted; a start with no requester, and the 404, are not) |
| A run's input is checked against the definition's input schema | Not tested: the check is in `StartRun` in `engine/engine.go`, and no test publishes a definition with an `input` schema |
| The same idempotency key returns the same run | `engine/engine_test.go:TestStartingTwiceWithTheSameKeyReturnsTheSameRun` (the engine is asserted; the 200 and `created: false` over the API are not) |
| A run has one of seven statuses | `engine/engine_test.go:TestARunThroughAllFiveKinds`, `engine/engine_test.go:TestRetriesThenBlocksThenResumes`, `engine/engine_test.go:TestCancelUndoesCompletedStepsInReverse`, `api/hooks_test.go:TestABlockedRunCanBeCancelledAndIsThenCountedAsFailed` (`compensating` is passed through and never observed by a test) |
| The first matching next rule decides the next step | `engine/engine_test.go:TestARunThroughAllFiveKinds`, `engine/engine_test.go:TestAnUnsureClassifierFallsBackToTheDefault`, `engine/engine_test.go:TestTheRequesterCannotApproveTheirOwnRun` |
| With no matching rule a run goes to the following step | `engine/engine_test.go:TestARunThroughAllFiveKinds` |
| A run ends at a step marked end, or after the last step | `engine/engine_test.go:TestARunThroughAllFiveKinds`, `engine/engine_test.go:TestContinueCarriesOnPastAFailedStep` (the status, the last event and the step that never ran are asserted; the run's output is not) |
| Each time a run reaches a step is a new visit | Not tested: visits are numbered in `createStep` in `engine/transitions.go`, and no test sends a run back to a step it has been through |
| A run's budget is a limit on what it spends | `engine/engine_test.go:TestARunStopsWhenItsBudgetIsSpent`, `engine/engine_test.go:TestARunThroughAllFiveKinds` (the block after an overspending step and the spend are asserted; the cost of failed attempts is not, and neither is that a run goes on past the minutes in its budget) |
| The budget is checked between steps | `engine/engine_test.go:TestARunStopsWhenItsBudgetIsSpent` (a step finishing and the run then blocking is asserted; the refusal to hand out a step, in `Claim` in `engine/transitions.go`, is not) |
