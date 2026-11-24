# Proof: audit-log

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| Every run has an ordered event log | `engine/engine_test.go:TestARunThroughAllFiveKinds`, `api/stream_test.go:TestARunCanBeFollowedAsAStream` (the order of types and the sequence numbers are asserted for a run that succeeds; the actor on an event, and the events of a failure, are not) |
| An event is written with the change it records | Not tested: every transition runs in one transaction, in `inTx` in `engine/engine.go`, and no test makes a transition fail half-way |
| The event log cannot be changed or deleted | `engine/engine_test.go:TestTheDatabaseRefusesToRewriteHistory` |
| Published versions cannot be changed or deleted | `engine/engine_test.go:TestTheDatabaseRefusesToRewriteHistory` (definition versions are asserted; template versions have the same trigger in `engine/store/migrations/00002_engine.sql` and are not) |
| Every model call is recorded | `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead`, `api/isolated_test.go:TestAnAgentInItsOwnContainerTalksOnlyToWeir` (one call with its template, its prompt and its cost is asserted; tokens, the reply, a failed attempt's call and the cut at 200,000 bytes are not) |
| Every tool call is recorded, allowed or refused | `api/tools_test.go:TestAnAgentReachesOnlyTheToolsItsStepLists` (the three statuses are asserted; the arguments, the result and the 16 KB limit are not) |
| A run's record can be read in one request | `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead`, `api/hooks_test.go:TestHooksTellTheApplicationWhatHappened` (model calls, hooks and the 404 are asserted; step and event times are not) |
| A run can be followed as a live stream | `api/stream_test.go:TestARunCanBeFollowedAsAStream` |
| A stream carries on from where the client stopped | `api/stream_test.go:TestARunCanBeFollowedAsAStream` (`Last-Event-ID` is asserted; `?after=` is not) |
| A workspace stream says which runs have moved | `api/stream_test.go:TestARunCanBeFollowedAsAStream` |
