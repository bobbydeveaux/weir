## ADDED Requirements

### Requirement: A handler waits for a pull request's checks
The deterministic handler `github.checks` SHALL read every check run and commit status on the head commit of the pull request given by `repo` and `pull_request`. While any is unfinished it SHALL ask to be run again after `every`, or 30 seconds when none is given. When all have finished it SHALL succeed with `state` (`passed`, `failed` or `none`), `head_sha`, and `failed`, a list of the checks that failed.

#### Scenario: Every check passes
- **WHEN** a pull request's head commit has two check runs, both finished with `success`
- **THEN** the step succeeds with `state: passed`, the head commit as `head_sha` and an empty `failed`

#### Scenario: A check is still running
- **WHEN** one check run is `in_progress`
- **THEN** the step is queued to run again, and uses none of its attempts

#### Scenario: A check fails
- **WHEN** all checks have finished and the check `test` ended with `failure`
- **THEN** the step succeeds with `state: failed`
- **AND** `failed` holds one entry with the name `test`, its address and its conclusion

#### Scenario: No check ran
- **WHEN** the head commit has no check runs and no statuses
- **THEN** the step succeeds with `state: none`

#### Scenario: Skipped and cancelled
- **WHEN** one check ended `skipped` and another `cancelled`
- **THEN** `state` is `failed`, and `failed` holds the cancelled check only

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

### Requirement: A handler merges a pull request whose checks passed
The deterministic handler `github.merge_pull_request` SHALL merge the pull request given by `repo` and `pull_request` with `method` (`merge`, `squash` or `rebase`; `merge` when none is given), and SHALL succeed with `merged: true` and the merge `commit`. It SHALL merge only when every check on the head commit has passed, and only at `head_sha` when one is given. It MUST NOT use an override of the repository's rules.

#### Scenario: A squash merge
- **WHEN** every check passed and the step gives `method: squash`
- **THEN** the pull request is merged as one commit
- **AND** the output holds `merged: true` and that commit

#### Scenario: The method is not one of the three
- **WHEN** the step gives `method: fast`
- **THEN** the step fails, naming the three methods

### Requirement: A merge that cannot be made is reported, not failed
When a check failed or is unfinished, when no check ran and the step does not set `allow_no_checks: true`, when the head commit is not `head_sha`, when the pull request is closed or already merged, or when GitHub refuses the merge, `github.merge_pull_request` SHALL succeed with `merged: false` and a `reason`, and SHALL merge nothing. A pull request that is already merged SHALL also give its merge commit.

#### Scenario: A check failed
- **WHEN** the check `test` failed on the head commit
- **THEN** the output is `merged: false` with a reason that names `test`, and the pull request is still open

#### Scenario: The branch moved
- **WHEN** the step gives a `head_sha` and the branch has a newer commit
- **THEN** the output is `merged: false` with a reason that says the branch moved

#### Scenario: A rule refuses
- **WHEN** the repository requires a review the pull request does not have
- **THEN** the output is `merged: false` with GitHub's reason

#### Scenario: No checks, and that is allowed
- **WHEN** the head commit has no checks and the step sets `allow_no_checks: true`
- **THEN** the pull request is merged

#### Scenario: Run twice
- **WHEN** the handler runs again after it merged
- **THEN** the output is `merged: false`, says the pull request was already merged, and holds the same commit

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
- **WHEN** `github.checks` and then `github.merge_pull_request` run
- **THEN** the first asks to read pull requests, checks, statuses and actions, and nothing it can write with
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
