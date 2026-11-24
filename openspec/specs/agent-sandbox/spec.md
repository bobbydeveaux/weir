# agent-sandbox Specification

## Purpose
An agent that runs beside Weir's worker is locked down: it gets nothing its step did not give it. This says what such an agent is denied, how a step gives it a shell or a working directory, and how Weir decides which of its changes to keep. An agent in a container of its own is a separate capability.

## Requirements

### Requirement: An agent beside the worker is never run with permission checks off
The worker SHALL run a model's command line with its permission checks on, in a mode that refuses whatever was not allowed in advance and never stops to ask. It SHALL NOT pass a flag that skips or bypasses those checks. Codex SHALL be run in a read-only sandbox.

#### Scenario: Claude is started for a step
- **WHEN** the worker builds Claude's command line for a step
- **THEN** it holds `--permission-mode dontAsk`
- **AND** it holds nothing that skips or bypasses permission checks

### Requirement: An agent has no built-in tool its step did not give it
An agent SHALL be started with every built-in tool of the command line switched off. A step that lists `files` gives it reading, and only in the directory that holds them. A step with `writes` or `workspace` gives it reading, writing and editing, and only in the place the step allows. A step with `shell` gives it a shell. Nothing else switches a built-in tool on.

#### Scenario: A step that gives nothing
- **WHEN** a step with no files, writes, workspace or shell runs
- **THEN** the command line is started with no built-in tools

#### Scenario: A step that gives documents
- **WHEN** a step lists one document
- **THEN** the agent may read, and only inside the directory that holds the document

### Requirement: An agent loads no settings and no tool server but Weir's
An agent beside the worker SHALL be started with no user or project settings files, with no MCP server other than the one Weir names for its step's tools, and without saving its session. It SHALL run in a directory made for the step and removed afterwards, so that nothing from a repository or a home directory changes how it behaves.

#### Scenario: Settings on the machine are ignored
- **WHEN** an agent runs beside the worker
- **THEN** the command line is told to load settings from nowhere
- **AND** its working directory is gone once it has finished

### Requirement: An agent sees almost none of the server's environment
The command line SHALL be given a short list of environment variables and not the worker's own: `PATH`, `HOME`, `USER`, `TMPDIR`, `LANG`, and the model's own sign-in. Any other variable reaches it only when whoever runs the server names it in `WEIR_AGENT_ENV`. The prompt SHALL be passed on standard input, so that it is not in a process listing.

#### Scenario: A secret in the server's environment
- **WHEN** the server's environment holds `WEIR_SECRET_THING`, and `WEIR_AGENT_ENV` names only `CLAUDE_CODE_USE_BEDROCK`
- **THEN** the agent's command line is given `CLAUDE_CODE_USE_BEDROCK`
- **AND** it is not given `WEIR_SECRET_THING`

### Requirement: A shell is only ever given inside a sandbox
An ai step with `shell:` asks for its agent to have a shell. The worker SHALL give one only when the server has `WEIR_AGENT_SHELL=on` and can run the shell in the command line's sandbox: on macOS, or on Linux with `bwrap` and `socat` installed. Otherwise the step SHALL fail with the error type `shell_unavailable` and a message saying why, and SHALL NOT be tried again. The worker SHALL never run a shell without a sandbox.

#### Scenario: The server does not allow a shell
- **WHEN** a step with `shell:` and `retry: { attempts: 3 }` runs on a server without `WEIR_AGENT_SHELL=on`
- **THEN** the step fails once, saying the server does not allow agents a shell
- **AND** the agent is not started

#### Scenario: Linux without the sandbox's programs
- **WHEN** a shell is asked for on a Linux worker that has no `bwrap`
- **THEN** the step is refused and no command line is started

### Requirement: A sandboxed shell writes only in its directory and reaches only listed hosts
The shell SHALL be able to write only inside the step's working directory, and to connect only to the hosts in `shell.hosts`. With none listed it SHALL have no network. It SHALL NOT be able to read `WEIR_SECRETS_DIR` or the places in a home directory that hold credentials, and it SHALL have no way to ask to leave the sandbox.

#### Scenario: One host is listed
- **WHEN** a step with `shell: { hosts: [proxy.golang.org] }` runs
- **THEN** the sandbox allows that host and no other
- **AND** the secrets directory, `~/.ssh` and `~/.aws` are among the paths it may not read

#### Scenario: No host is listed
- **WHEN** a step with `shell: {}` runs
- **THEN** the sandbox is given an empty list of hosts, and the shell has no network

### Requirement: Publishing checks a shell's hosts
Each entry in `shell.hosts` MUST be a host name, or a name beginning `*.`, which covers every host under it. Publishing SHALL refuse anything else, and more than 50 hosts.

#### Scenario: A host that is not a name
- **WHEN** a definition with `shell: { hosts: ["https://proxy.golang.org"] }` is published
- **THEN** publishing is refused, and the error says it is not a host name

### Requirement: A step's working directory is prepared by registered code
An ai step with `workspace:` SHALL name a registered handler, which fills the agent's working directory immediately before the agent starts. The handler SHALL be given the step's input and the fixed settings in `workspace.with`. Publishing SHALL refuse a handler that is not registered, and a step that sets both `workspace` and `files`.

#### Scenario: A directory is prepared from the step's input
- **WHEN** a step with `input: { repo: input.repo }` and `workspace: { handler: test.checkout, with: { depth: 1 } }` runs for the repository `acme/shop`
- **THEN** the handler is given the repository `acme/shop` and the depth 1

#### Scenario: Nothing is registered under the name
- **WHEN** a definition whose `workspace.handler` is not registered is published
- **THEN** publishing is refused

### Requirement: Weir keeps only what the agent changed
Weir SHALL record what a prepared directory holds before the agent starts, and compare afterwards. The step's files SHALL be the files the agent added, changed or deleted, with `deleted: true` on one that was removed. Paths the handler said to leave out are fixed before the agent starts, and a `.git` directory SHALL never be read back.

#### Scenario: An agent edits, adds and deletes
- **WHEN** an agent changes `main.go`, adds `docs/new.md`, deletes `old.go`, and also writes inside `.git` and an ignored `build` directory
- **THEN** the step's files are `main.go`, `docs/new.md` and `old.go`, the last marked deleted
- **AND** nothing from `.git` or `build` is among them

#### Scenario: The agent rewrites the ignore list
- **WHEN** an agent writes a `.gitignore` that names `docs/`
- **THEN** its change under `docs/` is still kept

### Requirement: The limits on a prepared directory apply to what changed
The limits in a step's `writes` SHALL apply to what the agent changed, not to what the directory held. With none set they are 20 files and 8 MB. Going over a limit, or adding a link or anything else that is not an ordinary file, SHALL fail the attempt.

#### Scenario: More changes than the step allows
- **WHEN** an agent changes four files in a large directory and its step allows three
- **THEN** the attempt fails, saying the agent changed more than 3 files

#### Scenario: A new link
- **WHEN** an agent adds a link to a file outside the directory
- **THEN** the attempt fails, saying it is not an ordinary file

### Requirement: A step can keep nothing of what its agent changed
With `workspace: { discard: true }`, the agent SHALL work in the prepared directory as usual, and nothing it changed there SHALL be kept.

#### Scenario: An assessment leaves no mark
- **WHEN** an agent in a step with `discard: true` changes two files
- **THEN** the step succeeds and has no files

### Requirement: A step says which paths its agent may change
`writes.only` SHALL list the only paths an agent may change, and `writes.never` paths it may not, both in the form a `.gitignore` uses. Unless the step sets `writes.protected: false`, `.git/`, `.github/workflows/`, `.claude/`, `CLAUDE.md` and `AGENTS.md` SHALL NOT be changed, at any depth. Protected wins over `never`, which wins over `only`.

#### Scenario: A workflow file
- **WHEN** an agent changes `.github/workflows/ci.yml` in a step that says nothing about protected paths
- **THEN** the step fails, saying the path is protected

#### Scenario: Never wins over only
- **WHEN** a step has `only: ["*.go"]` and `never: ["gen/"]`, and its agent changes `gen/types.go`
- **THEN** the change is refused as a path the step may never change

#### Scenario: A step lifts the protection
- **WHEN** a step sets `writes.protected: false` and its agent changes `CLAUDE.md`
- **THEN** the change is allowed

### Requirement: A forbidden change fails the step and keeps nothing
After the agent has exited, Weir SHALL check every path it wrote, changed or deleted against the step's rules. One change that is not allowed SHALL fail the attempt with the error type `path_forbidden`, and nothing the agent changed SHALL be kept. The agent is told the rules, and the check SHALL NOT depend on it having listened.

#### Scenario: A step that may write tests only
- **WHEN** an agent in a step with `writes: { only: ["*_test.go"] }` changes `api/server_test.go` and `api/server.go`
- **THEN** the step fails, saying `api/server.go` is not one of the paths the step may change
- **AND** neither file is kept
