# Proof: connections

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A connection is a named address in one workspace | `api/mcp_test.go:TestAnMCPConnectionsToolsBelongToItsWorkspaceOnly`, `api/hooks_test.go:TestHooksTellTheApplicationWhatHappened`, `api/githubapp_test.go:TestAGitHubAppConnectionSaysWhichSecretToSet` (the MCP tools, the 502, the unknown connection at publish and a second workspace's 404 are asserted; a missing `base_url` and an unknown kind are not) |
| A credential is named by reference and never stored | `api/credentials_test.go:TestAConnectionSignsEveryCallAndNeverKeepsTheSecret` |
| Malformed sign-in settings are refused when the connection is made | `api/credentials_test.go:TestAConnectionSignsEveryCallAndNeverKeepsTheSecret`, `internal/secrets/secrets_test.go:TestEachKindOfAuthSignsTheRequest`, `api/front_test.go:TestAConnectionCanSignInAsTheServer` |
| Each kind of auth signs a request in its own way | `internal/secrets/secrets_test.go:TestEachKindOfAuthSignsTheRequest`, `internal/secrets/secrets_test.go:TestAnIDTokenSignsInAsTheServerWithNoSecret`, `worker/githubapp_test.go:TestAGitHubAppSignsInWithATokenForOneRepository` |
| The credential is added to every call made through the connection | `api/credentials_test.go:TestAConnectionSignsEveryCallAndNeverKeepsTheSecret` (against a stand-in, not the real service) |
| A missing secret says where to put it and sends nothing | `internal/secrets/secrets_test.go:TestAMissingSecretSaysWhereToPutItAndSendsNothing`, `api/credentials_test.go:TestAConnectionSignsEveryCallAndNeverKeepsTheSecret` |
| A credential is not followed to another host | `internal/secrets/secrets_test.go:TestACredentialIsNotFollowedToAnotherHost` (the rule is asserted on a client that uses it; that every client Weir makes uses it is not) |
| A workspace reads only its own secrets | `internal/secrets/secrets_test.go:TestAWorkspaceReadsOnlyItsOwnSecrets`, `api/credentials_test.go:TestAConnectionSignsEveryCallAndNeverKeepsTheSecret` |
| File secrets stay inside the workspace's directory | `internal/secrets/secrets_test.go:TestFileSecretsStayInsideTheWorkspacesDirectory` |
| A connection can sign in as the server itself | `internal/secrets/secrets_test.go:TestAnIDTokenSignsInAsTheServerWithNoSecret`, `api/front_test.go:TestAConnectionCanSignInAsTheServer` (against a stand-in for the platform, not the real service) |
