# Proof: events-and-triggers

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| An application reports an event | `api/events_test.go:TestAnEventStartsEveryWorkflowWaitingForIt` (the 202, the reply and the unwanted event are asserted; an event with no type or with bad data is not) |
| A repeated event starts nothing new | `engine/waits_test.go:TestAStepWaitsForTheEventThatIsAboutItsRun`, `api/events_test.go:TestAnEventStartsEveryWorkflowWaitingForIt` (the repeat is asserted; an event with no id sent twice is not) |
| A trigger names the event that starts a workflow | `api/events_test.go:TestAnEventStartsEveryWorkflowWaitingForIt`, `api/schedule_test.go:TestAScheduleStartsARunWhenItIsDue` (the refusals are asserted; that an older version's trigger no longer matches is not) |
| An event starts every workflow whose trigger matches | `api/events_test.go:TestAnEventStartsEveryWorkflowWaitingForIt` (a run that could not start, and its `error`, is not asserted) |
| A trigger can filter events and shape the run's input | `api/events_test.go:TestAnEventStartsEveryWorkflowWaitingForIt`, `api/webhooks_test.go:TestASignedWebhookBecomesAnEvent` (a workflow with two triggers for one event is not tested) |
| A schedule trigger starts a workflow by the clock | `spec/schedule_test.go:TestASchedulePicksTheRightNextTime`, `api/schedule_test.go:TestAScheduleStartsARunWhenItIsDue` (a schedule with no `input`, and its `{}`, is not asserted) |
| A due schedule starts exactly one run | `api/schedule_test.go:TestAScheduleStartsARunWhenItIsDue` |
| A missed schedule fires once | `api/schedule_test.go:TestAScheduleStartsARunWhenItIsDue` (one overdue time is asserted to start one run and move to the future; a schedule that missed several times is not tested) |
| Events and triggers can be listed | `api/waits_test.go:TestASignedWebhookMovesARunThatIsWaiting`, `api/events_test.go:TestAnEventStartsEveryWorkflowWaitingForIt`, `api/schedule_test.go:TestAScheduleStartsARunWhenItIsDue`, `api/webhooks_test.go:TestASignedWebhookBecomesAnEvent` (the order, `limit=1` and `next_at` are asserted; the upper limit of 200 is not) |
| An event moves on the steps that were waiting for it | `engine/waits_test.go:TestAStepWaitsForTheEventThatIsAboutItsRun`, `api/waits_test.go:TestASignedWebhookMovesARunThatIsWaiting` (an event that both starts a workflow and resumes a step, and a rule that cannot be worked out, are not asserted) |
