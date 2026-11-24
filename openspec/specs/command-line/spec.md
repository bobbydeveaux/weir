# command-line Specification

## Purpose
The `weir` command sets a server up, starts it, and lets a person publish workflows, start runs and decide tasks from a terminal. This says which commands it has, what each checks, and how it exits.

## Requirements

### Requirement: Running weir with no command prints the usage
`weir` with no arguments SHALL print its usage to standard error and exit with code 2.

#### Scenario: No command
- **WHEN** `weir` is run with no arguments
- **THEN** the usage is printed to standard error
- **AND** the exit code is 2

### Requirement: An unknown command is refused
`weir` MUST refuse a command it does not have: it SHALL print an error that names the command, then the usage, to standard error, and exit with code 2.

#### Scenario: A command that does not exist
- **WHEN** `weir dance` is run
- **THEN** standard error names `"dance"`
- **AND** the exit code is 2

### Requirement: Help lists the commands and the settings
`weir help`, `weir -h` and `weir --help` SHALL print the usage to standard output and exit with code 0. The usage lists every command a person runs, with its arguments and flags, and the main settings read from the environment. `mcp` and `agent` are started by Weir for an agent and are not listed, and each MUST refuse to run without the address and token Weir gives it.

#### Scenario: Help is asked for
- **WHEN** `weir help` is run
- **THEN** standard output names `migrate`, `serve`, `publish`, `template`, `connection`, `run`, `status`, `tasks`, `approve`, `resume`, `key` and `version`
- **AND** the exit code is 0

#### Scenario: A person runs a command meant for an agent
- **WHEN** `weir agent` is run with neither `WEIR_URL` nor `WEIR_AGENT_TOKEN` set
- **THEN** the command fails and names both settings

### Requirement: Version prints the version
`weir version` SHALL print the program's version on one line and exit with code 0. A build that was not given a version prints `dev`.

#### Scenario: The version is asked for
- **WHEN** `weir version` is run
- **THEN** the version is printed and the exit code is 0

### Requirement: A command that fails says why and exits with 1
When a command fails, `weir` SHALL print `weir:` and the reason to standard error and exit with code 1. A command that succeeds exits with code 0.

#### Scenario: No database is named
- **WHEN** `weir migrate` is run with no database setting
- **THEN** standard error names `WEIR_DATABASE_URL`
- **AND** the exit code is 1

### Requirement: A command checks how many arguments it is given
Each command MUST be given exactly the arguments it takes: one for `publish`, `template`, `run`, `status`, `approve`, `resume`, `key create` and `key revoke`, two for `connection`, and none for `key list`. Any other number SHALL be refused before the database is opened, with a message that says how many the command takes. Flags may come before or after the arguments.

#### Scenario: Too few arguments
- **WHEN** `weir connection only-a-name` is run
- **THEN** the error says how many arguments `connection` takes
- **AND** the exit code is 1

### Requirement: Commands work on the database directly
Every command but `version` and `help` SHALL read the database from the environment and work on it directly. They do not call the HTTP API and need no API key. `publish`, `template`, `run`, `status`, `tasks`, `approve` and `resume` SHALL work in the `default` workspace. `connection` and `key` take `--workspace`, which defaults to `default`.

#### Scenario: A key before any key exists
- **WHEN** `weir key create my-app --workspace my-app` is run against a server's database that has no keys yet
- **THEN** the key is made, because the command needs no key of its own

### Requirement: A command records who ran it
`publish`, `template`, `run`, `approve`, `resume` and `key create` SHALL record an actor. The actor is the value of `--as`, and the value of `$USER` when `--as` is not given.

#### Scenario: A decision from the command line
- **WHEN** `weir approve <task-id> --as bob` is run
- **THEN** the decision is recorded as made by `bob`

### Requirement: Migrate brings the database up to date
`weir migrate` SHALL bring the database up to the latest schema and print the version it ended on, as `schema is at version N`.

#### Scenario: A database is migrated
- **WHEN** `weir migrate` is run against an empty database
- **THEN** the schema is at the latest version, and the version is printed

### Requirement: Serve runs the API and a worker
`weir serve` SHALL listen for the API and do the work of runs in one process, until it is interrupted. When it starts it SHALL print the address it listens on, the runner, whether API keys are required, where files are kept and where agents run. With `WEIR_WORKER=off` it SHALL answer the API and leave the work to another server on the same database. `WEIR_WORKER` MUST be `on`, `off` or not set, and any other value stops the server from starting.

#### Scenario: A server that only answers the API
- **WHEN** `weir serve` is started with `WEIR_WORKER=off`
- **THEN** it answers the API, starts no worker, and says the work is done by another server

#### Scenario: A value that is neither
- **WHEN** `weir serve` is started with `WEIR_WORKER=sometimes`
- **THEN** it refuses to start, and the error says the value must be `on` or `off`

### Requirement: Workflows, templates and connections are defined from the command line
`weir publish <file.yaml>` SHALL publish the workflow in the file. `weir template <key> --user <text>` SHALL publish a new version of a template, and MUST be given `--user`. `weir connection <name> <base-url>` SHALL create or update a connection. When a connection names a secret, the command SHALL say where the server reads it from and whether it is set, and never its value.

#### Scenario: A workflow is published
- **WHEN** `weir publish refund.yaml` is run with a valid workflow
- **THEN** it prints `published shop/refund version 1`
- **AND** it notes any parts that were accepted but are not enforced

#### Scenario: A template is published
- **WHEN** `weir template classify --user '{{input}}' --system 'Be brief.'` is run
- **THEN** it prints the key and the new version, as `classify@1`

#### Scenario: A template with no prompt
- **WHEN** `weir template classify` is run without `--user`
- **THEN** the command fails and says a template needs `--user`

#### Scenario: A connection's flags
- **WHEN** `weir connection` is run
- **THEN** it takes `--auth`, `--secret`, `--header`, `--username` and `--app` for how it signs in, `--mcp` and `--read-only` for an MCP server, and `--webhook`, `--webhook-secret`, `--webhook-header` and `--webhook-event-header` for webhooks

#### Scenario: A connection names a secret that is not set
- **WHEN** `weir connection github https://api.github.com --auth bearer --secret env:GITHUB_TOKEN` is run and the secret is not set
- **THEN** it prints the name of the variable the server reads, and that it is not set yet

### Requirement: Runs are started and inspected from the command line
`weir run <namespace>/<name>` SHALL start a run and print its id. `weir status <run-id>` SHALL print the run's status, its spend and a line for each step. `weir resume <run-id>` SHALL restart a blocked run. A workflow that is not named as `<namespace>/<name>`, and an `--input` that is not valid JSON, MUST be refused.

#### Scenario: A run is started
- **WHEN** `weir run shop/refund --input '{"order": "A-17"}'` is run
- **THEN** the new run's id is printed

#### Scenario: No input is given
- **WHEN** `weir run shop/refund` is run without `--input`
- **THEN** the run's input is `{}`

#### Scenario: The same idempotency key again
- **WHEN** `weir run shop/refund --key order-42` is run twice
- **THEN** the second prints the first run's id, and says it was already started with this key

#### Scenario: Input that is not JSON
- **WHEN** `weir run shop/refund --input 'order=A-17'` is run
- **THEN** the command fails and says `--input` is not valid JSON

#### Scenario: A run is inspected
- **WHEN** `weir status <run-id> --events` is run
- **THEN** it prints the run's status and spend, its error if it has one, each step's kind, status, attempts and result, and the event log

#### Scenario: A blocked run is given more budget
- **WHEN** `weir resume <run-id> --budget 5` is run
- **THEN** the run is restarted with a budget of 5 USD

### Requirement: Tasks are listed and decided from the command line
`weir tasks` SHALL list the steps waiting for a person, each with its task id, run, step, the role or user it is for and its outcomes, or say that nothing is waiting. `weir approve <task-id>` SHALL record a decision on a task. The outcome is `approved` unless `--outcome` names another, and `--note` adds a note.

#### Scenario: Nothing is waiting
- **WHEN** `weir tasks` is run and no task is open
- **THEN** it prints `nothing is waiting for a person`

#### Scenario: A task is rejected
- **WHEN** `weir approve <task-id> --outcome rejected --as bob` is run
- **THEN** it prints that `rejected` was recorded by `bob`

### Requirement: API keys are managed from the command line
`weir key create <name>` SHALL make a key for a workspace and print its secret once, with a note that it will not be shown again. It takes `--workspace`, `--role` (`writer` unless given) and `--act-as`. `weir key list` SHALL print each of the workspace's keys with its id, name, role and whether it is active or revoked. `weir key revoke <key-id>` SHALL stop a key working. `weir key` with no sub-command, or one it does not have, MUST be refused.

#### Scenario: A key is made
- **WHEN** `weir key create my-app --workspace my-app --role writer --act-as` is run
- **THEN** the key's secret is printed, beginning `weir_`

#### Scenario: A sub-command that does not exist
- **WHEN** `weir key rotate` is run
- **THEN** the command fails and names `rotate`
