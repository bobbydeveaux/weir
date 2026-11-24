# Proof: agent-tools

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| An agent is offered only the tools its step lists | `api/tools_test.go:TestAnAgentReachesOnlyTheToolsItsStepLists` |
| Every tool call is checked against the step's list | `api/tools_test.go:TestAnAgentReachesOnlyTheToolsItsStepLists`, `api/mcp_test.go:TestAnMCPConnectionsToolsBelongToItsWorkspaceOnly` |
| Every tool call is recorded | `api/tools_test.go:TestAnAgentReachesOnlyTheToolsItsStepLists`, `api/mcp_test.go:TestAnMCPConnectionsToolsBelongToItsWorkspaceOnly` (the tool and the status of each call are asserted; the arguments, result and duration are not, and neither is the 16 KB limit) |
| A tool call needs the token of a running attempt | `api/tools_test.go:TestAnAgentReachesOnlyTheToolsItsStepLists` |
| A tool's own failure is passed back to the agent | `api/tools_test.go:TestAnAgentReachesOnlyTheToolsItsStepLists` (the message and the `failed` record are asserted; that the step goes on is not) |
| A tool is a write unless it says it only reads | `worker/mcp_test.go:TestRemoteMCPToolsJoinTheRegistry`, `api/tools_test.go:TestPublishChecksTheToolsAStepLists` |
| Publishing checks the tools a step lists | `api/tools_test.go:TestPublishChecksTheToolsAStepLists`, `api/github_test.go:TestAnAgentReadsARepositoryAndProposesAChange` (the two refusals are asserted; a read tool listed under write is not) |
| A workspace's MCP servers give tools to that workspace only | `api/mcp_test.go:TestAnMCPConnectionsToolsBelongToItsWorkspaceOnly` |
| The HTTP tools go through a named connection | `api/tools_test.go:TestTheHTTPToolsGoThroughANamedConnection`, `api/credentials_test.go:TestAConnectionSignsEveryCallAndNeverKeepsTheSecret` (`http.get` is called; `http.post` is asserted only to be a write) |
| The GitHub tools read a repository and propose a change | `api/github_test.go:TestAnAgentReadsARepositoryAndProposesAChange` (against a stand-in for GitHub; the read tools have also met the real GitHub in `api/github_test.go:TestGitHubReadsLive`, which runs only when asked) |
| A GitHub tool stays in its repository and off the default branch | `api/github_test.go:TestAnAgentReadsARepositoryAndProposesAChange` (against a stand-in for GitHub) |
