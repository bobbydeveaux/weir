# Proof: github

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A connection can sign in as a GitHub App | `api/githubapp_test.go:TestAGitHubAppConnectionSaysWhichSecretToSet`, `worker/githubapp_test.go:TestAGitHubAppSignsInWithATokenForOneRepository`, `worker/githubapp_test.go:TestGitHubAppLive` (against a stand-in, not the real service, except the live test) |
| A token is minted for one repository with the least permission the call needs | `worker/githubapp_test.go:TestAGitHubAppSignsInWithATokenForOneRepository`, `worker/githubapp_test.go:TestEachGitHubCallAsksForTheLeastItNeeds` (against a stand-in, not the real service; the reuse is asserted, the ten minutes are not) |
| A check says whether Weir can sign in | `api/githubapp_test.go:TestAGitHubAppConnectionSaysWhichSecretToSet`, `worker/githubapp_test.go:TestAGitHubAppSignsInWithATokenForOneRepository` (the missing key, the 404 and a plain connection are asserted over the API; a check that succeeds is asserted below the API only, against a stand-in) |
| Only the GitHub tools and handlers can use a GitHub App connection | `worker/githubapp_test.go:TestAGitHubAppSignsInWithATokenForOneRepository` (the refusal to sign is asserted; a step that names such a connection is not run) |
| A repository is checked out for an agent | `api/githubcommit_test.go:TestAnAgentsChangesBecomeABranchAndAPullRequest`, `worker/githubgit_test.go:TestACheckoutLeavesNoTokenBehind`, `worker/githubgit_test.go:TestWhereARepositoryIsClonedFrom`, `worker/githubgit_test.go:TestGitHubCheckoutLive` (from a repository on disk, not the real service, except the live test; the limit on `depth`, hooks being off and submodules being left out are not asserted) |
| A checkout leaves no token behind | `worker/githubgit_test.go:TestACheckoutLeavesNoTokenBehind`, `worker/githubgit_test.go:TestGitHubCheckoutLive` |
| A commit turns a step's changes into a branch and a pull request | `api/githubcommit_test.go:TestAnAgentsChangesBecomeABranchAndAPullRequest`, `api/github_live_test.go:TestAnAgentOpensARealPullRequestLive` (against a stand-in, not the real service, except the live test; when the write token is asked for is not asserted) |
| A commit is safe to run twice | `api/githubcommit_test.go:TestAnAgentsChangesBecomeABranchAndAPullRequest`, `worker/githubcommit_test.go:TestACommitCannotBeUsedToWriteOutsideTheRepository` (against a stand-in, not the real service; a step that changed no files at all is not asserted) |
| A commit cannot write outside the repository | `worker/githubcommit_test.go:TestACommitCannotBeUsedToWriteOutsideTheRepository` |
| GitHub tools let an agent read a repository and propose a change | `api/github_test.go:TestAnAgentReadsARepositoryAndProposesAChange`, `api/github_test.go:TestGitHubReadsLive` (against a stand-in, not the real service; the live test covers the three read tools only, and the 200 KB limit is not asserted) |
| A connection can be pinned to one repository and the default branch cannot be written to | `api/github_test.go:TestAnAgentReadsARepositoryAndProposesAChange` (against a stand-in, not the real service) |
