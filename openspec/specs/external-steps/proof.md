# Proof: external-steps

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| An external step calls a named connection | `engine/engine_test.go:TestARunThroughAllFiveKinds`, `api/credentials_test.go:TestAConnectionSignsEveryCallAndNeverKeepsTheSecret` (the call, its credential and `secret_unavailable` are asserted; the default method and the three refusals at publishing are not) |
| What is sent is the step's input | `api/external_input_test.go:TestAnExternalStepSendsItsInput` |
| Every call carries an idempotency key | Not tested: the header is set in `worker/worker.go` (`external`), and no test reads `Idempotency-Key` from a request |
| A callback step hands the third party a one-time token | `engine/engine_test.go:TestARunThroughAllFiveKinds`, `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP` |
| A callback completes the step once | `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP`, `engine/engine_test.go:TestARunThroughAllFiveKinds` (the callback, the repeat and the made-up token are asserted; the 400 for a body that is not JSON, and a body that fails the output schema, are not) |
| A callback step must set a timeout | Not tested: the rule is in `spec/definition.go` (`validateExternal`), and the timer is set in `engine/transitions.go` (`Park`). `engine/engine_test.go:TestAHumanStepEscalatesAndThenTimesOut` proves the timer for a human step only |
| A poll step repeats the call until its condition holds | `engine/engine_test.go:TestAnExternalStepPollsUntilItsConditionHolds` (the five-second default is not asserted) |
| A resume mode is required | Not tested: the rules are in `spec/definition.go` (`validateExternal`), and no test publishes an external step that breaks them |
| A refused or failed call fails the attempt | Not tested: the handling is in `worker/worker.go` (`external`), and no test has a third party answer with an error status |
