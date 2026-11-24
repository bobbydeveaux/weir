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
| A poll step stops at its timeout | `engine/poll_timeout_test.go:TestAPollStepStopsAtItsTimeout`, `engine/poll_timeout_test.go:TestAPollStepThatFinishesInTimeIsNotStopped`, `engine/poll_timeout_test.go:TestAResumedPollStepGetsItsTimeoutAgain`, `engine/engine_test.go:TestAnExternalStepPollsUntilItsConditionHolds` (the default `on_failure` is asserted, `cancel` and `continue` are not; that a step runs over by no more than one interval is not asserted) |
| A resume mode is required | `engine/waits_test.go:TestPublishRefusesAWaitThatCouldTakeAnyEventOrWaitForEver` (`when` on a callback step is asserted; an unknown resume mode and the rules for `poll` are not) |
| A refused or failed call fails the attempt | Not tested: the handling is in `worker/worker.go` (`external`), and no test has a third party answer with an error status |
| A step can wait for an event that is about its run | `engine/waits_test.go:TestAStepWaitsForTheEventThatIsAboutItsRun`, `engine/waits_test.go:TestAnEventInAnotherWorkspaceMovesNothing`, `api/waits_test.go:TestASignedWebhookMovesARunThatIsWaiting` |
| The event a step takes becomes its output | `engine/waits_test.go:TestAStepWaitsForTheEventThatIsAboutItsRun`, `engine/waits_test.go:TestTheWaitForMergeExampleRuns` (the output and the routing are asserted; the actor recorded on the step is not) |
| A step that waits for an event must say which, and for how long | `engine/waits_test.go:TestPublishRefusesAWaitThatCouldTakeAnyEventOrWaitForEver`, `engine/waits_test.go:TestAStepThatWaitsForAnEventTimesOut` |
| An event that arrives before the step starts waiting is not missed | `engine/waits_test.go:TestAnEventThatArrivesBeforeTheStepStartsWaitingIsNotMissed`, `engine/waits_test.go:TestEventsAndWaitsThatRaceLoseNothing` (the limit of 1,000 is not asserted) |
| A run takes any one event once | `engine/waits_test.go:TestARunDoesNotTakeTheSameEventTwice` |
| A step may call a third party and then wait for its event | `engine/waits_test.go:TestAStepCanCallAThirdPartyAndThenWaitForItsEvent` |
