# Proof: workspaces

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| Every database has a default workspace | `engine/store/migrate_test.go:TestMigrateCreatesTheDefaultWorkspace`, `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead` |
| A request names its workspace | `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead`, `api/stream_test.go:TestARunCanBeFollowedAsAStream` (the header, the parameter and the 404 are asserted; the header winning over the parameter is not) |
| A workspace is created over the API | `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead` (the 201 is asserted; a refused slug, a missing name and creating one twice are not) |
| Workspaces can be listed | `api/auth_test.go:TestKeysDecideWhoMayDoWhat`, `api/front_test.go:TestBehindASignInProxy` (the list and a key seeing only its own are asserted; the order and the counts are not) |
| Definitions, templates, runs and tasks are kept apart | `api/views_test.go:TestWorkspacesAreKeptApartAndCanBeRead`, `api/auth_test.go:TestKeysDecideWhoMayDoWhat` |
| Events and schedules reach only their own workspace | `api/events_test.go:TestAnEventStartsEveryWorkflowWaitingForIt`, `api/schedule_test.go:TestAScheduleStartsARunWhenItIsDue`, `api/webhooks_test.go:TestASignedWebhookBecomesAnEvent` |
| Connections, tools and secrets belong to one workspace | `api/mcp_test.go:TestAnMCPConnectionsToolsBelongToItsWorkspaceOnly`, `api/credentials_test.go:TestAConnectionSignsEveryCallAndNeverKeepsTheSecret`, `internal/secrets/secrets_test.go:TestAWorkspaceReadsOnlyItsOwnSecrets`, `internal/secrets/secrets_test.go:TestFileSecretsStayInsideTheWorkspacesDirectory` (no test lists one workspace's plain HTTP connections from another) |
| A run's files and streams are kept apart | `api/files_test.go:TestAnAgentsFilesAreKeptForLaterStepsAndTheApplication`, `api/stream_test.go:TestARunCanBeFollowedAsAStream` |
