# Proof: isolated-agents

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A setting sends every agent to a container of its own | `api/isolated_test.go:TestAnAgentInItsOwnContainerTalksOnlyToWeir` (with a stand-in for the container; that the worker's own runner is not used is asserted, and reading `WEIR_AGENT_RUNNER` and refusing to start, in `cmd/weir/main.go`, is not tested) |
| The container is given Weir's address and the token for its step | `api/isolated_test.go:TestAnAgentInItsOwnContainerTalksOnlyToWeir`, `worker/kubernetes_test.go:TestAnAgentIsStartedAsAKubernetesJob`, `worker/cloudrun_test.go:TestAnAgentIsStartedAsARunOfACloudRunJob` (against a stand-in, not the real service) |
| The agent fetches its job and its directory from the API | `api/isolated_test.go:TestAnAgentInItsOwnContainerTalksOnlyToWeir` (the prepared directory, the recorded prompt, and that the job carries what the run has left as the most the call may spend are asserted; the 30 MB limit is not) |
| The agent in a container is given every tool | `runner/claude_test.go:TestAnAgentInItsOwnContainerIsGivenEveryTool`, `runner/claude_test.go:TestAnAgentInItsOwnContainerBypassesPermissionChecksUnlessTold` (the command line's arguments are asserted, with no real agent; which variables it is given is not) |
| A setting keeps the agent's permission checks on | `runner/claude_test.go:TestAnAgentInItsOwnContainerCanKeepPermissionChecksOn`, `runner/claude_test.go:TestAnAgentInItsOwnContainerBypassesPermissionChecksUnlessTold`, `cmd/weir/main_test.go:TestAnAgentRefusesPermissionsItDoesNotKnow` (the command line's arguments and the refusal are asserted, with no real agent; that a real command line under a policy that forbids bypassing then lets the agent work was checked by hand outside a container, not by a test) |
| The agent sends one result | `api/isolated_test.go:TestAnAgentInItsOwnContainerTalksOnlyToWeir` (a result over 30 MB is not tested) |
| Weir checks again everything a container sends | `api/isolated_test.go:TestAnAgentInItsOwnContainerTalksOnlyToWeir` (too many files, a path outside and the recorded cost are asserted; too many bytes, files with no `writes`, and a missing output are not) |
| The token opens nothing once the step ends | `api/isolated_test.go:TestAnAgentInItsOwnContainerTalksOnlyToWeir` |
| An agent can be started with Docker | `api/isolated_test.go:TestAnAgentRunsInItsOwnContainerLive` (live: runs only with real credentials) |
| An agent can be started as a Kubernetes Job | `worker/kubernetes_test.go:TestAnAgentIsStartedAsAKubernetesJob`, `api/isolated_test.go:TestAnAgentRunsInItsOwnContainerLive` (against a stand-in, not the real service; the live test starts a real Job only when it is given a cluster, and that has been a local one) |
| An agent can be started as a run of a Cloud Run job | `worker/cloudrun_test.go:TestAnAgentIsStartedAsARunOfACloudRunJob` (against a stand-in, not the real service: it has never been run on the real Cloud Run) |
