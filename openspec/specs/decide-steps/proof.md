# Proof: decide-steps

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A decide step chooses one label from a closed list | `engine/engine_test.go:TestARunThroughAllFiveKinds`, `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP` (the label and the route it takes are asserted; the fields of the output are not) |
| An unsure answer becomes the default | `engine/engine_test.go:TestAnUnsureClassifierFallsBackToTheDefault` |
| A failed or wrong answer becomes the default | `engine/engine_test.go:TestAnUnsureClassifierFallsBackToTheDefault` (a classifier that fails is asserted; an answer that is not a label is not, and neither are `confident` and `reason`) |
| The engine refuses a label that is not on the list | Not tested: the check is in `engine/transitions.go` (`completeTx`), and no test completes a decide step with a label that is not one of its labels |
| Publishing checks a decide step | `spec/definition_test.go:TestValidateFindsEveryProblem`, `engine/engine_test.go:TestPublishRefusesWhatCannotRun` (the default, the pinned template and the missing template are asserted; fewer than two labels and a confidence out of range are not) |
| Labels may carry descriptions | `spec/provider_test.go:TestDescriptionsMustBelongToLabels`, `runner/decisions_test.go:TestJevDecides` (descriptions sent to a language model are not asserted) |
| The provider is chosen per step | `spec/provider_test.go:TestADecideStepMayNameItsProvider`, `runner/decisions_test.go:TestRouterSendsADecisionToTheNamedProvider` (the routing rule is asserted; reading `WEIR_DECIDE_PROVIDER` and `WEIR_TYPESAFE_KEY` in `cmd/weir/main.go` is not) |
| A language model is given the labels as a closed list | `runner/claude_test.go:TestClaudeDecideAsksForOneOfTheLabels` (Claude is asserted; Codex is covered only by `runner/codex_test.go:TestCodexLive`, which runs only with real credentials) |
| A decision model answers over HTTP with an API key | `runner/decisions_test.go:TestJevDecides`, `runner/decisions_test.go:TestGeminiDecisionDecides`, `runner/decisions_test.go:TestDecisionServiceErrorsAreReported` (against stand-in servers; a reply with no answer or no confidence is not asserted, and neither service has been called with a real key) |
| Every decision is recorded as a model call | `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead`, `runner/decisions_test.go:TestJevDecides` (one call with its template is asserted in the record, and a cost worked out from tokens at the runner; the estimated mark and the other fields of the record are not) |
