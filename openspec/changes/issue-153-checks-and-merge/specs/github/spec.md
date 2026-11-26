## ADDED Requirements

### Requirement: A handler waits for a pull request's required checks
The deterministic handler `github.checks` SHALL wait for the checks the base branch's rules require (branch protection and rulesets), and any the step names in `checks`, on the pull request's head commit. While any is unfinished it SHALL ask to be run again after `every`, or 30 seconds. When all have finished it SHALL succeed with `state` (`passed`, `failed` or `none`), `head_sha`, `mergeable_state`, and `failed`, the required or named checks that failed.

#### Scenario: Every required check passes
- **WHEN** the base branch requires the checks `test` and `scan`, and both finished with `success` on the head commit
- **THEN** the step succeeds with `state: passed`, the head commit as `head_sha` and an empty `failed`

#### Scenario: A required check is still running
- **WHEN** the required check `test` is `in_progress`
- **THEN** the step is queued to run again, and uses none of its attempts

#### Scenario: A required check fails
- **WHEN** all required checks have finished and `test` ended with `failure`
- **THEN** the step succeeds with `state: failed`
- **AND** `failed` holds one entry with the name `test`, its address and its conclusion

#### Scenario: Nothing is required
- **WHEN** the base branch requires no checks and the step names none
- **THEN** the step succeeds with `state: none`

### Requirement: A check that isn't required decides nothing
A check that is neither required by the base branch's rules nor named in `checks` SHALL NOT hold up `github.checks` or make it fail, and SHALL be listed under `other` with its conclusion. A required or named check SHALL pass only on `success`.

#### Scenario: A check that isn't required fails
- **WHEN** the required checks passed and an optional check `lint-experimental` failed
- **THEN** `state` is `passed`, and `lint-experimental` is listed under `other` with `failure`

#### Scenario: A required check was skipped
- **WHEN** the required check `test` ended `skipped`
- **THEN** `state` is `failed`, and `failed` holds `test` with `skipped`

### Requirement: A step waiting on checks is woken by GitHub's webhooks
When the connection receives GitHub webhooks, a `github.checks` step that is waiting SHALL be run again as soon as a `check_run`, `check_suite` or `status` event arrives for its pull request's head commit, without waiting for `every`. Polling SHALL remain as the fallback. Each poll SHALL wait longer than the last, up to ten times `every`. When GitHub's rate-limit headers say the token's limit is low, the handler SHALL wait until the limit resets.

#### Scenario: A check finishes
- **WHEN** a step is waiting with `every: 5m` and a `check_run` `completed` event for its head commit arrives after 20 seconds
- **THEN** the handler runs within seconds of the event, not after five minutes

#### Scenario: No webhooks
- **WHEN** the connection receives no webhooks
- **THEN** the handler runs again at `every`, then at longer intervals, until the checks finish or the step's `timeout` passes

#### Scenario: Near the rate limit
- **WHEN** GitHub answers with `x-ratelimit-remaining: 5`
- **THEN** the next run waits until the time in `x-ratelimit-reset`

### Requirement: A failed check carries its log and how it stands on the base branch
Each entry in `failed` SHALL hold the end of the job's log as `log`, at most 16 KiB, when the check is a GitHub Actions job whose log the connection may read. It SHALL hold `fails_on_base`: `true` when the newest finished check of the same name on the base branch's head also failed, and `false` when it passed. Both SHALL be left out when they cannot be read. The output SHALL NOT be larger than 256 KiB.

#### Scenario: The base branch fails the same check
- **WHEN** the check `scan` failed on the pull request and its newest run on the base branch's head also failed
- **THEN** its entry has `fails_on_base: true`

#### Scenario: The pull request broke it
- **WHEN** the check `test` failed on the pull request and passed on the base branch's head
- **THEN** its entry has `fails_on_base: false`

#### Scenario: A long log
- **WHEN** a failed job's log is 1 MiB
- **THEN** its entry's `log` is the last 16 KiB of it

#### Scenario: The log cannot be read
- **WHEN** the App has no permission to read the job's log
- **THEN** the step still succeeds, and the entry has no `log`

#### Scenario: The log is marked as text from outside
- **WHEN** a failed check's entry holds a `log` and a `summary`
- **THEN** both are marked as text from outside, as `ai-steps` describes

### Requirement: Merging is allowed only by the connection
`github.merge_pull_request` and `github.enable_auto_merge` SHALL run only through a connection that sets `allow_merge: true`, which is false unless set. Otherwise they SHALL refuse before sending anything to GitHub, with an error that names the setting. Whoever manages the connection decides whether Weir may merge, not a definition's author. A merged pull request is the only way they change the default branch.

#### Scenario: A connection that doesn't allow merging
- **WHEN** a step calls `github.merge_pull_request` through a connection without `allow_merge`
- **THEN** the step fails with an error that names `allow_merge`, and nothing is sent to GitHub

#### Scenario: A connection that allows merging
- **WHEN** the same step runs through a connection with `allow_merge: true`, and the pull request can be merged
- **THEN** the pull request is merged

#### Scenario: Writing to the default branch is still refused
- **WHEN** an agent calls `github.put_file` with the default branch, on a connection with `allow_merge: true`
- **THEN** the call is refused and the default branch is unchanged

### Requirement: A handler merges a pull request whose required checks passed
The deterministic handler `github.merge_pull_request` SHALL merge the pull request given by `repo` and `pull_request` with `method`, which the step MUST give (`merge`, `squash` or `rebase`), and SHALL succeed with `merged: true` and the merge `commit`. It SHALL merge only when every required check on the head commit passed, and only at `head_sha` when given. It MUST NOT use an override of the repository's rules, so a required review still applies.

#### Scenario: A squash merge
- **WHEN** every required check passed and the step gives `method: squash`
- **THEN** the pull request is merged as one commit
- **AND** the output holds `merged: true` and that commit

#### Scenario: No method
- **WHEN** a definition's `github.merge_pull_request` step gives no `method`
- **THEN** publishing is refused, naming the three methods

#### Scenario: The method is not one of the three
- **WHEN** the step gives `method: fast`
- **THEN** publishing is refused, naming the three methods

### Requirement: A merge records what it checked
The event of a `github.merge_pull_request` step SHALL record the required checks it saw, with their conclusions, and the head commit it merged at, or would have merged at when it answered `merged: false`.

#### Scenario: The evidence for a merge
- **WHEN** the handler merges after `test` and `scan` passed at commit `abc123`
- **THEN** the step's event lists `test` and `scan` with `success`, and `abc123`

### Requirement: A merge that cannot be made is reported, not failed
`github.merge_pull_request` SHALL succeed with `merged: false` and a `reason`, and SHALL merge nothing, when:
- a required check failed or is unfinished;
- no check is required and the step does not set `allow_no_checks: true`;
- the head commit is not `head_sha`;
- the pull request is closed or already merged;
- GitHub refuses the merge, for example because a required review is missing.

A pull request that is already merged SHALL also give its merge commit.

#### Scenario: A required check failed
- **WHEN** the required check `test` failed on the head commit
- **THEN** the output is `merged: false` with a reason that names `test`, and the pull request is still open

#### Scenario: The branch moved
- **WHEN** the step gives a `head_sha` and the branch has a newer commit
- **THEN** the output is `merged: false` with a reason that says the branch moved

#### Scenario: A rule refuses
- **WHEN** the repository requires a review the pull request does not have
- **THEN** the output is `merged: false` with GitHub's reason

#### Scenario: No checks, and that is allowed
- **WHEN** no check is required and the step sets `allow_no_checks: true`
- **THEN** the pull request is merged

#### Scenario: Run twice
- **WHEN** the handler runs again after it merged
- **THEN** the output is `merged: false`, says the pull request was already merged, and holds the same commit

### Requirement: A handler turns on GitHub's auto-merge
The deterministic handler `github.enable_auto_merge` SHALL turn on GitHub's auto-merge for the pull request, with the `method` the step MUST give. GitHub then merges it once every rule of the base branch is met, including required checks and reviews, and Weir does not wait or poll for it. The handler SHALL succeed with `enabled: true`, or `enabled: false` and GitHub's reason. A later step MAY wait for the `pull_request` event to learn the outcome.

#### Scenario: Auto-merge with a review still needed
- **WHEN** the base branch requires one approving review and passing checks, and the handler turns on auto-merge
- **THEN** the output is `enabled: true`
- **AND** GitHub merges the pull request only after the checks pass and a person approves it

#### Scenario: Auto-merge not allowed
- **WHEN** the repository has auto-merge turned off
- **THEN** the output is `enabled: false` with GitHub's reason, and nothing changes

### Requirement: A commit can add to a branch that exists
With `update: true`, `github.commit` SHALL, when `branch` already exists, clone that branch, apply the step's files and deletions, and commit and push them when they change something. The push MUST NOT be forced. The output SHALL hold the pull request already open for the branch, and `pushed: false` when the files changed nothing.

#### Scenario: A second commit
- **WHEN** the branch `weir/fix-1` has an open pull request, and the handler runs with `update: true` and a file that differs from the branch
- **THEN** the branch has one more commit, and the same pull request holds it
- **AND** the output holds `pushed: true`, the new commit and that pull request

#### Scenario: Run twice with the same files
- **WHEN** the handler runs again with `update: true` and the same files
- **THEN** nothing is pushed and the output holds `pushed: false`

#### Scenario: The branch does not exist yet
- **WHEN** the handler runs with `update: true` for a branch that is not there
- **THEN** it makes the branch from the base and opens a pull request, as without `update`

## MODIFIED Requirements


### Requirement: A token is minted for one repository with the least permission the call needs
For a call through a GitHub App connection, Weir SHALL ask GitHub for a token limited to the one repository being worked on and to the least permission that call needs. A token SHALL be held in memory only, reused for the same repository and the same need for ten minutes, and MUST NOT be given to an agent. A repository the App is not installed on, or a permission the App does not have, SHALL give an error that names it.

#### Scenario: Reading a file
- **WHEN** a file of `acme/app` is read through a GitHub App connection
- **THEN** GitHub is asked for a token for the repository `app` alone, with `contents: read` and `metadata: read`

#### Scenario: What each call needs
- **WHEN** calls are made to read a file or clone, to read a pull request, to create a branch or write a file, and to open a pull request or comment
- **THEN** they ask, in that order, to read contents, to read pull requests, to write contents, and to write pull requests

#### Scenario: Reading checks and merging
- **WHEN** `github.checks`, then `github.merge_pull_request` or `github.enable_auto_merge`, run
- **THEN** the first asks to read pull requests, checks, statuses, actions and administration (for the base branch's rules), and nothing it can write with
- **AND** the second asks also to write contents and pull requests

#### Scenario: A read, a second read, then a write
- **WHEN** two reads of one repository are followed by a commit to it
- **THEN** the second read asks GitHub for no new token
- **AND** the commit asks for a new one, with its own permissions

#### Scenario: The App lacks a permission
- **WHEN** `github.checks` runs through an App that may not read checks
- **THEN** the step fails with an error that names `checks: read`

### Requirement: A commit is safe to run twice
Running `github.commit` again for the same branch SHALL push nothing and return the pull request that is already open, unless the step sets `update: true` and the files change something on that branch. When the step changed no files, or the files make no difference, it SHALL say so with `changed: false` and open nothing.

#### Scenario: The same branch again
- **WHEN** the handler runs a second time for `weir/fix-1`
- **THEN** its output says nothing was pushed and returns the pull request already open

#### Scenario: The files make no difference
- **WHEN** the files to commit are the same as on the base branch
- **THEN** nothing is committed or pushed
