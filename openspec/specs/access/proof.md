# Proof: access

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| Without an admin key the API trusts its callers | `api/api_test.go:TestAWorkflowDrivenEntirelyOverHTTP` |
| With an admin key set every request needs a key | `api/auth_test.go:TestKeysDecideWhoMayDoWhat` (the 401 is asserted; the `WWW-Authenticate` header is not) |
| A key is shown once and only its hash is stored | `api/auth_test.go:TestKeysDecideWhoMayDoWhat` |
| A key belongs to one workspace | `api/auth_test.go:TestKeysDecideWhoMayDoWhat` |
| A role decides what a key may do | `api/auth_test.go:TestKeysDecideWhoMayDoWhat`, `api/front_test.go:TestBehindASignInProxy` (a reader's 403, a writer's 403 on keys, a made-up role and who creates a workspace are asserted; a writer deciding, resuming and cancelling is not) |
| A key acts as itself unless it may act for others | `api/auth_test.go:TestKeysDecideWhoMayDoWhat` (the admin key naming a person is not asserted) |
| A revoked key stops working at once | `api/auth_test.go:TestKeysDecideWhoMayDoWhat` |
| Some paths need no key | `api/auth_test.go:TestKeysDecideWhoMayDoWhat`, `api/isolated_test.go:TestAnAgentInItsOwnContainerTalksOnlyToWeir`, `api/webhooks_test.go:TestASignedWebhookBecomesAnEvent` (the health check, an agent's paths and webhooks are asserted on a server with keys on; callbacks and tool calls are only tested on a server with keys off) |
| A sign-in proxy can vouch for a person | `api/front_test.go:TestBehindASignInProxy` (who the person is and what they may do are asserted; that their address is the actor recorded on a run is not) |
| A server can be made to refuse to start open | `cmd/weir/main_test.go:TestServeChecksTheAdminKeyBeforeAnythingElse`, `cmd/weir/main_test.go:TestEitherSettingMeetsTheNeedForAnAdminKey` (the refusal with no key, and either setting meeting the need, are asserted through `weir serve`; that the image sets the variable in `deploy/Dockerfile`, and the warning a server with no key prints, are not tested, because no test starts a server) |
| The admin key can be read from a file | `cmd/weir/main_test.go:TestTheAdminKeyCanBeReadFromAFile`, `cmd/weir/main_test.go:TestTheAdminKeySettingsAreRefusedWhenWrong`, `cmd/weir/main_test.go:TestServeChecksTheAdminKeyBeforeAnythingElse` (the key read and trimmed, and each refusal with no key in its error, are asserted; that a request with the key from a file is let in is not tested on its own, because the key is handed to the API exactly as `WEIR_ADMIN_KEY` is) |
