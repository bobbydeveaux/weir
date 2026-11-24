# Proof: workers-and-tokens

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A worker claims one ready step at a time | `engine/engine_test.go:TestAKilledWorkerDoesNotLoseOrRepeatAStep`, `engine/engine_test.go:TestARunThroughAllFiveKinds` (the second worker given nothing and `attempt.started` are asserted; the order steps are handed out in is not) |
| Each attempt has a one-time token, stored only as a hash | `engine/engine_test.go:TestAKilledWorkerDoesNotLoseOrRepeatAStep` (the earlier token being refused is asserted; that only a hash is stored is not: it is in `Claim` in `engine/transitions.go` and the `attempts` table) |
| A token finishes its attempt once | `engine/engine_test.go:TestARunThroughAllFiveKinds`, `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP`, `engine/engine_test.go:TestAKilledWorkerDoesNotLoseOrRepeatAStep` |
| A worker holds a step under a lease and extends it with heartbeats | Not tested: `Heartbeat` is in `engine/transitions.go` and the worker's loop in `RunOnce` in `worker/worker.go`, and no test runs a step for longer than its lease |
| A vanished worker's step is run again without using a retry | `engine/engine_test.go:TestAKilledWorkerDoesNotLoseOrRepeatAStep` |
| A step whose workers keep vanishing fails | Not tested: the limit is `MaxAbandoned` in `engine/engine.go`, applied in `failTx` in `engine/transitions.go`, and no test abandons a step more than once |
| The sweep acts on everything time has caught up with | `engine/engine_test.go:TestAKilledWorkerDoesNotLoseOrRepeatAStep`, `engine/engine_test.go:TestAHumanStepEscalatesAndThenTimesOut` (an expired lease and a timeout are asserted; a timer whose attempt has finished, and two sweeps at once, are not) |
| A step has at most one live attempt | Not tested: the rule is the unique index `attempts_one_live` in `engine/store/migrations/00002_engine.sql`, and no test tries to insert a second live attempt |
| An attempt can wait, or go back in the queue, without using a retry | `engine/engine_test.go:TestARunThroughAllFiveKinds`, `engine/engine_test.go:TestAnExternalStepPollsUntilItsConditionHolds` |
