# Proof: human-steps

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A human step waits in an inbox | `engine/engine_test.go:TestARunThroughAllFiveKinds`, `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP` |
| A decision is one of the step's outcomes | `engine/engine_test.go:TestTheRequesterCannotApproveTheirOwnRun` |
| The requester cannot decide their own run | `engine/engine_test.go:TestTheRequesterCannotApproveTheirOwnRun`, `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP` |
| A decision routes the run | `engine/engine_test.go:TestTheRequesterCannotApproveTheirOwnRun` |
| A task is decided once | `engine/engine_test.go:TestTheRequesterCannotApproveTheirOwnRun` |
| An undecided step escalates | `engine/engine_test.go:TestAHumanStepEscalatesAndThenTimesOut` |
| An undecided step times out | `engine/engine_test.go:TestAHumanStepEscalatesAndThenTimesOut` |
| A human step says who decides | Not tested: the rule is in `spec/definition.go` (`validateHuman`), and no test publishes a human step without an assignee |
