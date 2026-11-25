# Proof: examples

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| The examples run as they are written, with no key | `engine/examples_test.go:TestTheSupportInboxExampleSendsAnApprovedReply`, `engine/examples_test.go:TestTheBugAt2amExampleFixesDeploysAndConfirms`, `engine/examples_test.go:TestTheVulnerableLibraryExampleEndsWhenTheScannerSaysSo`, `engine/waits_test.go:TestTheWaitForMergeExampleRuns` (each reads its file from `examples/`; the templates are stand-ins, not the ones in the READMEs, and the READMEs' commands are not run) |
| The support inbox sends only a reply that someone else approved | `engine/examples_test.go:TestTheSupportInboxExampleSendsAnApprovedReply`, `engine/examples_test.go:TestTheSupportInboxExampleFilesNoiseWithoutAnAgent`, `engine/examples_test.go:TestTheSupportInboxExampleFilesARejectedReply` |
| The bug at 2am is fixed only with approval, and confirmed by the error tracker | `engine/examples_test.go:TestTheBugAt2amExampleFixesDeploysAndConfirms`, `engine/examples_test.go:TestTheBugAt2amExampleGivesAnUnsureCallToAPerson`, `engine/examples_test.go:TestTheBugAt2amExamplePagesWhenTheFixIsRejected` |
| The vulnerable library is finished when the scanner says so | `engine/examples_test.go:TestTheVulnerableLibraryExampleEndsWhenTheScannerSaysSo`, `engine/examples_test.go:TestTheVulnerableLibraryExampleStopsWhenThePlanIsRejected` (the 15 minutes between two questions to the scanner are passed by moving the step's time in the database) |
