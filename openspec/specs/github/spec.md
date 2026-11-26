# github Specification

## Purpose
GitHub is the first third party Weir knows well. A connection can sign in as a GitHub App, a repository can be checked out for an agent and its changes committed as a pull request, and agents are given tools to read a repository and propose a change.

## Requirements

### Requirement: A connection can sign in as a GitHub App
A connection with `auth.kind: github-app` SHALL name the App, by its client id or App id in `app`, and a reference to the App's private key in `secret`. Weir SHALL read the key when it needs to sign in and MUST NOT store it. Weir SHALL reply 400 to such a connection with no `app`, and to an `mcp` connection that tries to sign in this way. A key that is missing, is not a private key, or is not the App's SHALL give an error that says which, and never repeats the secret.

#### Scenario: A connection with no App
- **WHEN** a connection is made with `auth: {kind: github-app, secret: env:GITHUB_APP_KEY}` and no `app`
- **THEN** the reply is 400

#### Scenario: The secret is not a key
- **WHEN** the secret a GitHub App connection names holds text that is not a private key
- **THEN** signing in fails with an error that does not hold that text

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
- **THEN** the first asks to read pull requests, checks, statuses and actions, and nothing it can write with
- **AND** the second asks also to write contents and pull requests

#### Scenario: A read, a second read, then a write
- **WHEN** two reads of one repository are followed by a commit to it
- **THEN** the second read asks GitHub for no new token
- **AND** the commit asks for a new one, with its own permissions

#### Scenario: The App lacks a permission
- **WHEN** `github.checks` runs through an App that may not read checks
- **THEN** the step fails with an error that names `checks: read`

### Requirement: A check says whether Weir can sign in
`POST /v1/connections/{name}/check` SHALL report, for a GitHub App connection, whether Weir can sign in, without changing anything on GitHub. A missing key SHALL NOT fail the check: the reply says what to set. For a connection of any other kind it SHALL reply `checked: false`. A connection in another workspace SHALL be answered with 404.

#### Scenario: Weir can sign in
- **WHEN** a GitHub App connection whose key is set and right is checked
- **THEN** the reply holds the App's name, its owner, its permissions and where it is installed

#### Scenario: The key is not set yet
- **WHEN** the workspace `acme` checks a GitHub App connection whose secret `env:GITHUB_APP_KEY` is not set
- **THEN** the reply is 200 with `checked: true`, `ok: false` and a `detail` naming `WEIR_SECRET__ACME__GITHUB_APP_KEY`

#### Scenario: Another workspace asks
- **WHEN** another workspace checks `acme`'s connection
- **THEN** the reply is 404

### Requirement: Only the GitHub tools and handlers can use a GitHub App connection
An external step, a hook, a fetched document or a plain HTTP tool that names a GitHub App connection SHALL be refused, and its request MUST NOT be signed or sent with the App's key.

#### Scenario: A plain call names the connection
- **WHEN** a plain HTTP request is signed for a GitHub App connection
- **THEN** signing fails and the request has no `Authorization` header

### Requirement: A repository is checked out for an agent
The workspace handler `github.checkout` SHALL clone one repository into an agent's working directory before the agent runs. It SHALL ask for a token that can only read that one repository, from a GitHub App connection or a bearer token; a connection with no auth clones without one. Git SHALL run with no configuration but Weir's own, with hooks off and without submodules.

#### Scenario: An agent starts inside the checkout
- **WHEN** a step with `workspace: { handler: github.checkout }` runs
- **THEN** its agent finds the repository's files in its working directory

#### Scenario: The step names only the repository
- **WHEN** the step's input gives `repo` as `owner/name` and no `ref`, and its `with` gives no `connection` and no `depth`
- **THEN** the default branch is cloned through the connection `github`, with a depth of 1

#### Scenario: A branch is named
- **WHEN** the step's input gives `ref: feature`, a branch or a tag
- **THEN** the directory holds that branch's files

#### Scenario: Too much history is asked for
- **WHEN** the step's `with` gives a `depth` above 1000
- **THEN** the step fails

### Requirement: A checkout leaves no token behind
The token SHALL be given to git for the one command through its environment. After the checkout it MUST NOT be in any file in the directory, in the repository's configuration or in the remote's address, and an error from git MUST NOT hold it. Every `.gitignore` in the repository, at any depth, SHALL decide which paths are left out of the agent's changes, each applying under its own directory.

#### Scenario: The directory is searched for the token
- **WHEN** a repository is cloned with a token
- **THEN** no file in the directory holds the token, and the repository's configuration holds no credential

#### Scenario: A nested ignore file
- **WHEN** `web/.gitignore` says `build/`
- **THEN** `web/build/out.js` is left out of the agent's changes and `build/out.js` is not

### Requirement: A commit turns a step's changes into a branch and a pull request
The deterministic handler `github.commit` SHALL take the files a step changed (`from`), clone the repository afresh, apply the files and deletions, commit them, push a new `branch` and open a pull request with `title` and `body`. It SHALL use nothing from the directory the agent worked in, and SHALL ask for a token that can write only at this point. It SHALL refuse to commit to the base branch.

#### Scenario: An agent's changes are proposed
- **WHEN** a step changed one file, added one and deleted one, and `github.commit` runs with the branch `weir/fix-1`
- **THEN** the remote has that branch with exactly those changes
- **AND** the base branch is unchanged and one pull request is open
- **AND** the output holds the branch, the base, the commit and the pull request's number and address

#### Scenario: The optional inputs are left out
- **WHEN** the handler is given no `base`, no `message` and no `author`
- **THEN** the base is the repository's default branch, the commit's message is the title, and its author is `weir`

#### Scenario: The branch is the base
- **WHEN** the handler is asked to commit to `main`, the default branch
- **THEN** the step fails, saying the changes must go on a new branch

### Requirement: A commit is safe to run twice
Running `github.commit` again for the same branch SHALL push nothing and return the pull request that is already open, unless the step sets `update: true` and the files change something on that branch. When the step changed no files, or the files make no difference, it SHALL say so with `changed: false` and open nothing.

#### Scenario: The same branch again
- **WHEN** the handler runs a second time for `weir/fix-1`
- **THEN** its output says nothing was pushed and returns the pull request already open

#### Scenario: The files make no difference
- **WHEN** the files to commit are the same as on the base branch
- **THEN** nothing is committed or pushed

### Requirement: A commit cannot write outside the repository
`github.commit` SHALL refuse a file whose path leaves the repository, whose path has a `.git` directory anywhere in it, or whose path goes through a link in the repository, for a write or a deletion alike. One refused file SHALL stop the commit, and nothing SHALL be written or removed outside the clone.

#### Scenario: A path through a link
- **WHEN** the repository holds a link `elsewhere` that points outside it, and a file `elsewhere/planted.txt` is to be committed
- **THEN** the commit is refused and no file is written where the link points

#### Scenario: A hook is planted
- **WHEN** a file `.git/hooks/pre-commit` is to be committed
- **THEN** the commit is refused

### Requirement: GitHub tools let an agent read a repository and propose a change
Weir SHALL offer seven GitHub tools to agents, three of class `read` and four of class `write`, each through a named connection (`github` if left out) that supplies the credential. Publishing SHALL refuse a step that lists a write tool as a read. A write SHALL be safe to repeat. The token MUST NOT appear in a recorded tool call.

#### Scenario: The read tools
- **WHEN** an agent calls `github.get_file`, `github.list_files` and `github.get_pull_request`
- **THEN** it gets one text file of up to 200 KB, the entries of one directory, and a pull request's title, description, branches and changed files

#### Scenario: The write tools
- **WHEN** an agent calls `github.create_branch`, `github.put_file`, `github.open_pull_request` and `github.comment`
- **THEN** a branch is created, one file is written to it as a commit, a pull request is opened and a comment is added

#### Scenario: A write is repeated
- **WHEN** an agent creates the branch `fix/typo`, writes a file to it and opens a pull request, and then does all three again
- **THEN** the branch and the pull request that already exist are returned, and writing the same content changes nothing
- **AND** the second answers are `created: false`, `changed: false` and `opened: false` with the same pull request number

#### Scenario: A file that is not there
- **WHEN** an agent reads `nope.md`
- **THEN** the tool call fails, saying the file is not in the repository

### Requirement: A connection can be pinned to one repository and the default branch cannot be written to
A connection whose address ends `/repos/<owner>/<name>` SHALL work on that repository only: a call that names another SHALL be refused. A connection that is not pinned SHALL refuse a call that does not name a repository. `github.put_file` SHALL refuse to write to the repository's default branch, and SHALL send nothing to GitHub when it refuses.

#### Scenario: Another repository on a pinned connection
- **WHEN** an agent asks a connection pinned to `acme/app` for a file in `acme/payroll`
- **THEN** the call is refused, saying the connection is for `acme/app`

#### Scenario: A write to the default branch
- **WHEN** an agent calls `github.put_file` with the branch `main`, the default branch
- **THEN** the call is refused and the file on `main` is unchanged

### Requirement: A handler waits for a pull request's checks
The deterministic handler `github.checks` SHALL wait for the checks that count on the pull request's head commit: with `count: required`, the default, those GitHub says are required for that pull request, and with `count: all`, every check run and commit status. Any the step names in `checks` count too. While any is unfinished it SHALL ask to be run again. When all have finished it SHALL succeed with `state` (`passed`, `failed` or `none`), `head_sha`, `mergeable_state`, and `failed`.

#### Scenario: Every required check passes
- **WHEN** GitHub requires the checks `test` and `scan` for the pull request, and both finished with `success` on the head commit
- **THEN** the step succeeds with `state: passed`, the head commit as `head_sha` and an empty `failed`

#### Scenario: A required check is still running
- **WHEN** the required check `test` is `in_progress`
- **THEN** the step is queued to run again, and uses none of its attempts

#### Scenario: A required check fails
- **WHEN** all required checks have finished and `test` ended with `failure`
- **THEN** the step succeeds with `state: failed`
- **AND** `failed` holds one entry with the name `test`, its address and its conclusion

#### Scenario: Nothing is required
- **WHEN** GitHub requires no checks for the pull request, the step names none, and `count` is not given
- **THEN** the step succeeds with `state: none`

#### Scenario: Every check counts, on a repository with no rules
- **WHEN** the base branch has no rules, the step gives `count: all`, and the check `deploy` ended with `failure`
- **THEN** the step succeeds with `state: failed`, and `failed` holds `deploy`

#### Scenario: A count that is not one of the two
- **WHEN** a definition's `github.checks` step gives `count: some`
- **THEN** publishing is refused, naming `required` and `all`

### Requirement: Which checks count, and what passes
Whether a check is required SHALL be GitHub's own answer for that pull request, which covers branch protection and rulesets. A required check SHALL pass as GitHub passes it: on `success`, `skipped` or `neutral`. A check that counts only because of `count: all` SHALL pass on the same three. A check the step names in `checks` SHALL pass only on `success`. A check that does not count SHALL NOT hold the step up or fail it, and SHALL be listed under `other` with its conclusion.

#### Scenario: A check that isn't required fails
- **WHEN** `count` is `required`, the required checks passed and an optional check `lint-experimental` failed
- **THEN** `state` is `passed`, and `lint-experimental` is listed under `other` with `failure`

#### Scenario: A required job was skipped by its own condition
- **WHEN** the required check `build` ended `skipped`, which GitHub accepts for a merge
- **THEN** `build` counts as passed

#### Scenario: A named check was skipped
- **WHEN** the step gives `checks: [test]` and `test` ended `skipped`
- **THEN** `state` is `failed`, and `failed` holds `test` with `skipped`

#### Scenario: Cancelled, with every check counting
- **WHEN** `count` is `all`, one check ended `skipped` and another `cancelled`
- **THEN** `state` is `failed`, and `failed` holds the cancelled check only

### Requirement: With every check counting, the handler waits for checks to appear
With `count: all`, a head commit that has no check yet SHALL NOT be reported as `none` until `settle` has passed since the step first ran, or one minute when none is given. Until then the handler SHALL ask to be run again.

#### Scenario: Straight after a push
- **WHEN** the step first runs two seconds after the push, and no check has been registered
- **THEN** the step is queued to run again

#### Scenario: A repository with no pipeline
- **WHEN** a minute has passed since the step first ran and the head commit still has no check
- **THEN** the step succeeds with `state: none`

### Requirement: A waiting handler polls less often, and keeps within GitHub's limits
While checks are unfinished, `github.checks` SHALL run again after `every`, or 30 seconds when none is given, and each wait SHALL be longer than the last, up to ten times `every`. When GitHub's rate-limit headers say the token's limit is low, the handler SHALL wait until the limit resets. `mergeable_state` SHALL be GitHub's own value, and `unknown` when GitHub has not worked it out.

#### Scenario: A slow pipeline
- **WHEN** a step with `every: 30s` waits on a pipeline that takes an hour
- **THEN** the waits grow to five minutes and stay there

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

### Requirement: A handler merges a pull request whose checks passed
The deterministic handler `github.merge_pull_request` SHALL merge the pull request given by `repo` and `pull_request` with `method`, which the step MUST give (`merge`, `squash` or `rebase`), and SHALL succeed with `merged: true` and the merge `commit`. It SHALL merge only when every check that counts passed, by `count` and `checks` as `github.checks` reads them, and only at `head_sha` when given. It MUST NOT use an override of the repository's rules.

#### Scenario: A squash merge
- **WHEN** every check that counts passed and the step gives `method: squash`
- **THEN** the pull request is merged as one commit
- **AND** the output holds `merged: true` and that commit

#### Scenario: No method
- **WHEN** a definition's `github.merge_pull_request` step gives no `method`
- **THEN** publishing is refused, naming the three methods

#### Scenario: The method is not one of the three
- **WHEN** the step gives `method: fast`
- **THEN** publishing is refused, naming the three methods

### Requirement: A merge records what it checked
The output of a `github.merge_pull_request` step SHALL record, as `checks`, the checks that counted, with their conclusions, and as `head_sha` the head commit it merged at, or would have merged at when it answered `merged: false`. The output is kept with the run, so the evidence for a merge can be read afterwards.

#### Scenario: The evidence for a merge
- **WHEN** the handler merges after `test` and `scan` passed at commit `abc123`
- **THEN** the step's output lists `test` and `scan` with `success`, and `abc123`

### Requirement: A merge that cannot be made is reported, not failed
`github.merge_pull_request` SHALL succeed with `merged: false` and a `reason`, and SHALL merge nothing, when:
- a check that counts failed or is unfinished;
- no check counts and the step does not set `allow_no_checks: true`;
- the head commit is not `head_sha`;
- the pull request is closed or already merged;
- GitHub refuses the merge, for example because a required review is missing.

A pull request that is already merged SHALL also give its merge commit.

#### Scenario: A required check failed
- **WHEN** the required check `test` failed on the head commit
- **THEN** the output is `merged: false` with a reason that names `test`, and the pull request is still open

#### Scenario: A red pipeline on a repository with no rules
- **WHEN** the base branch has no rules, the step gives `count: all`, and the check `deploy` failed
- **THEN** the output is `merged: false` with a reason that names `deploy`

#### Scenario: The branch moved
- **WHEN** the step gives a `head_sha` and the branch has a newer commit
- **THEN** the output is `merged: false` with a reason that says the branch moved

#### Scenario: A rule refuses
- **WHEN** the repository requires a review the pull request does not have
- **THEN** the output is `merged: false` with GitHub's reason

#### Scenario: No checks, and that is allowed
- **WHEN** no check counts and the step sets `allow_no_checks: true`
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
