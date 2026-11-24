# Proof: expressions

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| Expressions are CEL and are checked when a definition is published | `spec/definition_test.go:TestValidateFindsEveryProblem`, `spec/expr_test.go:TestASampleOfNoneOrAllAndARateThatIsNotOne` (a `next` rule and a wrongly typed call are asserted; a bad expression in a mapping, a poll condition or a trigger is not) |
| An expression sees the run's input, its earlier steps and the run | `spec/definition_test.go:TestResolveEvaluatesStringsAndKeepsTheRest`, `spec/provider_test.go:TestAnExpressionCanFallBackWhenADecisionCarriesNoData`, `engine/engine_test.go:TestARunThroughAllFiveKinds`, `spec/expr_test.go:TestASampleIsAFractionOfRunsAndTheSameOnesEveryTime` (`input`, `steps.<id>.output` and `run.id` are asserted; `run.requester`, `label`, `outcome` and `files` under `steps`, and the latest visit, are not) |
| A next rule also sees what the step has just produced | `engine/engine_test.go:TestARunThroughAllFiveKinds`, `engine/engine_test.go:TestTheRequesterCannotApproveTheirOwnRun` (`label` and `outcome` are asserted; `output` in a rule is not) |
| A condition must be true or false | Not tested: the check is `EvalBool` in `spec/expr.go` and the run is failed in `advance` in `engine/transitions.go`, and no test runs a rule that gives something other than true or false |
| An input mapping is resolved before its step starts | `spec/definition_test.go:TestResolveEvaluatesStringsAndKeepsTheRest`, `api/external_input_test.go:TestAnExternalStepSendsItsInput` (the mapping is asserted; `input_invalid` is not) |
| A step with no mapping is given a default input | `api/external_input_test.go:TestAnExternalStepSendsItsInput` (the empty input of an external step is asserted; a model step given the run's input is not) |
| A trigger's expressions see the event | `api/events_test.go:TestAnEventStartsEveryWorkflowWaitingForIt` |
| A poll condition sees the reply | `engine/engine_test.go:TestAnExternalStepPollsUntilItsConditionHolds`, `api/external_input_test.go:TestAnExternalStepSendsItsInput` |
| A duration is a number and a unit | `spec/definition_test.go:TestParseDuration` (the forms are asserted; publishing refusing a bad duration, and a negative one, are not) |
| The sample function picks a fixed fraction of seeds | `spec/expr_test.go:TestASampleIsAFractionOfRunsAndTheSameOnesEveryTime`, `spec/expr_test.go:TestASampleOfNoneOrAllAndARateThatIsNotOne` |
