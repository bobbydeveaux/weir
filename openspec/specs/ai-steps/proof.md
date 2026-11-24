# Proof: ai-steps

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| An ai step runs a pinned prompt template | `api/workspace_test.go:TestAStepsDirectoryIsPreparedAndItsChangesAreKept` (filling `{{input}}` from the step's input is asserted; refusing an ai prompt that is not pinned or does not exist is not, though the same rule is asserted for a decide template in `spec/definition_test.go:TestValidateFindsEveryProblem`) |
| An ai step must declare an output schema and a budget | `spec/definition_test.go:TestAnAIStepNeedsSchemaAndBudget` |
| The engine checks an agent's output against the schema | `engine/engine_test.go:TestOutputThatFailsItsSchemaIsRejected`, `runner/claude_test.go:TestClaudeRunParsesOutputCostAndUsage`, `runner/claude_test.go:TestClaudeRunFailsWithoutStructuredOutput` |
| A step's budget is a limit on what its agent may spend | `runner/claude_test.go:TestClaudeRunParsesOutputCostAndUsage`, `api/codex_budget_test.go:TestACodexStepIsHeldToItsBudgetOrRefused` (passing the budget to Claude's command line is asserted, and so is the `budget_exceeded` failure, without a second try, for a Codex step whose estimate passed its budget; `budget.minutes` is not) |
| Every model call is recorded with its cost | `api/isolated_test.go:TestAnAgentInItsOwnContainerTalksOnlyToWeir`, `engine/engine_test.go:TestARunThroughAllFiveKinds` (the prompt and the cost in the record, and the run's total, are asserted; the other fields are not, and nor is the cost of a failed attempt) |
| An agent may read only the documents its step lists | `runner/claude_test.go:TestAnAgentGivenFilesMayReadOnlyThose`, `api/credentials_test.go:TestAConnectionSignsEveryCallAndNeverKeepsTheSecret` (the read rule and the fetch from a connection are asserted; `file_unavailable` and the 20 MB limit are not) |
| Files an agent writes are kept for later steps | `api/files_test.go:TestAnAgentsFilesAreKeptForLaterStepsAndTheApplication`, `runner/claude_test.go:TestAnAgentThatMayWriteIsConfinedToOneDirectory` |
| What an agent writes is limited | `runner/claude_test.go:TestAnAgentThatMayWriteIsConfinedToOneDirectory`, `api/files_test.go:TestAStepMayOnlyReadTheFilesOfAStepThatWrites`, `api/files_test.go:TestAnAgentsFilesAreKeptForLaterStepsAndTheApplication` |
| The step's tier and the template's model and effort are acted on | `runner/claude_test.go:TestAModelCanBeNamedAsACloudServesIt`, `runner/codex_test.go:TestByTierSendsEachStepToItsRunner`, `runner/workspace_test.go:TestAPassThatKeepsNothingAndItsEffort` (the tier's model, routing by tier and passing the effort are asserted; refusing a template with an unknown effort is not) |
| Publishing names the parts of a definition that are not acted on | `spec/definition_test.go:TestUnsupportedNamesWhatIsAcceptedButNotEnforced`, `api/api_test.go:TestPublishingSaysWhatIsNotEnforced` (every entry of the list is asserted where it is built; the reply to a publish is asserted for `tiers` and `budget.minutes` only) |
