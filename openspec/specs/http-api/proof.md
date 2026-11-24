# Proof: http-api

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| The API says it is up | `api/auth_test.go:TestKeysDecideWhoMayDoWhat`, `api/front_test.go:TestBehindASignInProxy` (the 200 is asserted; the body is not) |
| Definitions are published and read | `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP`, `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead` (the 201, the version, the list and the source are asserted; `not_yet_enforced` and `?version=` are not) |
| Templates are published and read | `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP`, `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead` (publishing and the list are asserted; the 400 for a missing prompt and `GET /v1/templates/{key}` are not) |
| A run is started | `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP`, `engine/engine_test.go:TestStartingTwiceWithTheSameKeyReturnsTheSameRun` (the 201 is asserted over HTTP; the same run coming back for the same key is asserted on the engine, and the 200 is not) |
| Runs are read, listed and counted | `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP`, `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead`, `api/auth_test.go:TestKeysDecideWhoMayDoWhat` (the filters, the record and the usage totals are asserted; the limit of 200 is not) |
| A run is resumed or cancelled | `api/hooks_test.go:TestABlockedRunCanBeCancelledAndIsThenCountedAsFailed`, `engine/engine_test.go:TestRetriesThenBlocksThenResumes` (cancelling and its 403 are asserted over HTTP; resuming is asserted on the engine, and its 200 and 409 are not) |
| Tasks are listed and decided | `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP`, `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead` (the 200 and the 403 are asserted; the 409 for a closed task is not asserted over HTTP) |
| Events are reported and listed | `api/events_test.go:TestAnEventStartsEveryWorkflowWaitingForIt` |
| Connections are made, listed and checked | `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP`, `api/mcp_test.go:TestAnMCPConnectionsToolsBelongToItsWorkspaceOnly`, `api/githubapp_test.go:TestAGitHubAppConnectionSaysWhichSecretToSet`, `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead` (a missing `base_url` and an unknown `kind` are not asserted) |
| A callback completes a waiting step | `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP` (the 200 and both 409s are asserted; the 400 for a body that is not JSON is not) |
| A run's files are listed and downloaded | `api/files_test.go:TestAnAgentsFilesAreKeptForLaterStepsAndTheApplication`, `api/workspace_test.go:TestAStepsDirectoryIsPreparedAndItsChangesAreKept` |
| Runs can be followed as streams | `api/stream_test.go:TestARunCanBeFollowedAsAStream` (`Last-Event-ID` is asserted; `?after=` is not) |
| A refusal has a status code and a reason | `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP`, `api/auth_test.go:TestKeysDecideWhoMayDoWhat`, `api/tools_test.go:TestAnAgentReachesOnlyTheToolsItsStepLists` (400, 401, 403, 404 and 409 are asserted; 413, 422 and the shape of the error body are not) |
| The API can also answer under a prefix | `api/front_test.go:TestBehindASignInProxy` |
