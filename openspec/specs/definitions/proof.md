# Proof: definitions

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A definition is YAML and an unknown key is refused | `spec/definition_test.go:TestParseRejectsUnknownKeys` |
| A definition names its contract version, its namespace and its name | `spec/definition_test.go:TestValidateFindsEveryProblem` (the contract version and the unknown kind are asserted; the patterns for names and ids, the need for one step and the repeated id are not) |
| A step uses only the fields of its kind | `spec/definition_test.go:TestValidateFindsEveryProblem` |
| Publishing reports every problem it finds | `spec/definition_test.go:TestAnAIStepNeedsSchemaAndBudget`, `spec/definition_test.go:TestValidateFindsEveryProblem` |
| Every reference must resolve when a definition is published | `engine/engine_test.go:TestPublishRefusesWhatCannotRun`, `spec/definition_test.go:TestValidateFindsEveryProblem`, `spec/definition_test.go:TestValidateChecksHandlersWhenAsked` (a handler, a template and a `next` target are asserted; a missing connection, an unregistered undo handler and a schema that does not compile are not) |
| Each publish makes a new numbered version that cannot be changed | `engine/engine_test.go:TestPublishingAgainMakesANewVersion`, `engine/engine_test.go:TestTheDatabaseRefusesToRewriteHistory`, `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead` |
| The live alias points at the newest version | `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead` (the live version after a first publish is asserted; that the alias moves on a second publish, and that a run stays on its version, are not) |
| A template is published as numbered versions | `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP`, `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead` (publishing, listing and the workspace boundary are asserted; the second version number, the `effort` rule and the database refusing a change to a template version are not) |
| A model step names a pinned template version | `spec/definition_test.go:TestValidateFindsEveryProblem`, `engine/engine_test.go:TestPublishRefusesWhatCannotRun` (the refusals are asserted; that a step is given the version it names after a newer one is published is not) |
| Publishing says what it accepts but does not enforce | Not tested: the list is built by `Unsupported` in `spec/definition.go` and returned as `not_yet_enforced` by the publish handler in `api/api.go`, and no test publishes a definition that sets one of these |
