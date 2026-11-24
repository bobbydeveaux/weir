# Proof: hooks

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A workflow declares its hooks | `api/hooks_test.go:TestHooksTellTheApplicationWhatHappened`, `api/hooks_test.go:TestAHookCanBeSignedSoTheReceiverKnowsItIsFromWeir` (an unknown connection, a malformed `on` and a secret that is a path are asserted; a missing connection, a path with no slash and an empty `on` are not) |
| A hook reports each event it asked for | `api/hooks_test.go:TestHooksTellTheApplicationWhatHappened` (the types, step, run, input and workflow are asserted; `seq`, `actor`, `data` and `at` are not, and nothing tests that a hook is not queued when its event is rolled back) |
| The worker delivers hooks through the connection | `api/hooks_test.go:TestHooksTellTheApplicationWhatHappened`, `api/credentials_test.go:TestAConnectionSignsEveryCallAndNeverKeepsTheSecret` (a different id per hook is asserted; the same id on a second attempt is not) |
| A failed delivery is retried with a growing wait | `api/hooks_test.go:TestHooksTellTheApplicationWhatHappened` (one refusal, the wait and the retry are asserted; that the wait doubles and that Weir gives up after eight attempts are not) |
| A failed hook does not fail the run | `api/hooks_test.go:TestHooksTellTheApplicationWhatHappened` (the run succeeds after a refused hook; a hook that is given up on for good is not tested) |
| A hook can be signed so the receiver knows it is from Weir | `api/hooks_test.go:TestAHookCanBeSignedSoTheReceiverKnowsItIsFromWeir` |
| A hook whose secret is not set is not sent unsigned | `api/hooks_test.go:TestAHookCanBeSignedSoTheReceiverKnowsItIsFromWeir` |
| Hooks are listed in a run's record | `api/hooks_test.go:TestHooksTellTheApplicationWhatHappened` (the type, attempts and delivery time are asserted; the other fields are not) |
