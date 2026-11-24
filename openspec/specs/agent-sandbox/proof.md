# Proof: agent-sandbox

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| An agent beside the worker is never run with permission checks off | `runner/claude_test.go:TestClaudeIsNeverRunWithPermissionChecksOff`, `runner/codex_test.go:TestCodexIsLockedDown`, `runner/claude_test.go:TestAShellIsOnlyEverGivenInsideASandbox` |
| An agent has no built-in tool its step did not give it | `runner/claude_test.go:TestClaudeIsNeverRunWithPermissionChecksOff`, `runner/claude_test.go:TestAnAgentGivenFilesMayReadOnlyThose`, `runner/claude_test.go:TestAnAgentThatMayWriteIsConfinedToOneDirectory`, `runner/claude_test.go:TestAShellIsOnlyEverGivenInsideASandbox` |
| An agent loads no settings and no tool server but Weir's | `runner/workspace_test.go:TestAPassThatKeepsNothingAndItsEffort`, `runner/claude_test.go:TestClaudeRunParsesOutputCostAndUsage`, `runner/claude_test.go:TestAnAgentGivenFilesMayReadOnlyThose` (the flags, and the removal of a directory that held files, are asserted; the throwaway directory of a step with no files is not) |
| An agent sees almost none of the server's environment | `runner/claude_test.go:TestOnlyNamedVariablesReachAnAgent`, `runner/claude_test.go:TestClaudeRunParsesOutputCostAndUsage` (Claude is asserted; the list Codex is given is not) |
| A shell is only ever given inside a sandbox | `runner/claude_test.go:TestAShellIsOnlyEverGivenInsideASandbox`, `api/shell_test.go:TestAStepsShellReachesItsAgentAndARefusalIsNotRetried` (the refusal and that it is not retried are asserted; the error type `shell_unavailable` is not) |
| A sandboxed shell writes only in its directory and reaches only listed hosts | `runner/claude_test.go:TestAShellIsOnlyEverGivenInsideASandbox` (the settings handed to the command line's sandbox are asserted. That the sandbox then holds is proven only by `runner/claude_test.go:TestAnAgentsShellIsSandboxedLive`, which runs only with real credentials and has been run on macOS, not Linux) |
| Publishing checks a shell's hosts | `api/shell_test.go:TestAStepsShellReachesItsAgentAndARefusalIsNotRetried` (three malformed hosts are asserted to be refused, and a `*.` name to be accepted; the limit of 50 hosts is not) |
| A step's working directory is prepared by registered code | `api/workspace_test.go:TestAStepsDirectoryIsPreparedAndItsChangesAreKept` |
| Weir keeps only what the agent changed | `runner/workspace_test.go:TestAnAgentInAPreparedDirectoryReturnsOnlyWhatItChanged`, `api/workspace_test.go:TestAStepsDirectoryIsPreparedAndItsChangesAreKept` |
| The limits on a prepared directory apply to what changed | `runner/workspace_test.go:TestAnAgentInAPreparedDirectoryReturnsOnlyWhatItChanged`, `api/workspace_test.go:TestAStepsDirectoryIsPreparedAndItsChangesAreKept` (the file limit, the link and the defaults are asserted; the size limit is not) |
| A step can keep nothing of what its agent changed | `runner/workspace_test.go:TestAPassThatKeepsNothingAndItsEffort` |
| A step says which paths its agent may change | `spec/writes_test.go:TestWhichPathsAnAgentMayChange`, `api/workspace_test.go:TestAChangeToAForbiddenPathFailsTheStepAndKeepsNothing` |
| A forbidden change fails the step and keeps nothing | `api/workspace_test.go:TestAChangeToAForbiddenPathFailsTheStepAndKeepsNothing` (the failure, its message, that the agent is told the rules and that nothing is kept are asserted; the error type `path_forbidden` is not) |
