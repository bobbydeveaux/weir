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
For a call through a GitHub App connection, Weir SHALL ask GitHub for a token limited to the one repository being worked on and to the least permission that call needs. A token SHALL be held in memory only, reused for the same repository and the same need for ten minutes, and MUST NOT be given to an agent. A repository the App is not installed on SHALL give an error that names it.

#### Scenario: Reading a file
- **WHEN** a file of `acme/app` is read through a GitHub App connection
- **THEN** GitHub is asked for a token for the repository `app` alone, with `contents: read` and `metadata: read`

#### Scenario: What each call needs
- **WHEN** calls are made to read a file or clone, to read a pull request, to create a branch or write a file, and to open a pull request or comment
- **THEN** they ask, in that order, to read contents, to read pull requests, to write contents, and to write pull requests

#### Scenario: A read, a second read, then a write
- **WHEN** two reads of one repository are followed by a commit to it
- **THEN** the second read asks GitHub for no new token
- **AND** the commit asks for a new one, with its own permissions

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
Running `github.commit` again for the same branch SHALL push nothing and return the pull request that is already open. When the step changed no files, or the files make no difference, it SHALL say so with `changed: false` and open nothing.

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
