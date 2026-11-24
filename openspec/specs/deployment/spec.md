# deployment Specification

## Purpose
Deployment covers what an operator needs to run Weir: the settings it reads from the environment, bringing the database up to date, the container images, `docker compose`, and the Helm chart.

## Requirements

### Requirement: A database must be named, as one address or in parts
Weir SHALL read its Postgres database from `WEIR_DATABASE_URL`. When that is not set it SHALL build the address from `WEIR_DATABASE_HOST` with whichever of `WEIR_DATABASE_PORT`, `_NAME`, `_USER`, `_PASSWORD` and `_SSLMODE` are set, and the host may be a directory that holds a socket. A password may hold any character. When both are set the one address wins. With neither, a command that needs the database MUST fail and name both settings.

#### Scenario: No database is named
- **WHEN** Weir is started with neither `WEIR_DATABASE_URL` nor `WEIR_DATABASE_HOST`
- **THEN** it fails with an error that names `WEIR_DATABASE_URL` and `WEIR_DATABASE_HOST`

#### Scenario: The database in parts
- **WHEN** `WEIR_DATABASE_HOST`, `_NAME`, `_USER` and a `_PASSWORD` holding a quote, a space and a backslash are set
- **THEN** Weir connects with those values, and the password is passed on whole

### Requirement: The address to listen on comes from the environment
The server SHALL listen on `WEIR_HTTP_ADDR`. When that is not set it SHALL listen on the port in `PORT`, and when neither is set, on `:8080`.

#### Scenario: A platform names the port
- **WHEN** `PORT=9000` is set and `WEIR_HTTP_ADDR` is not
- **THEN** the server listens on `:9000`

#### Scenario: Both are set
- **WHEN** `PORT=9000` and `WEIR_HTTP_ADDR=:7000` are set
- **THEN** the server listens on `:7000`

### Requirement: An unknown runner is refused
`WEIR_RUNNER` MUST be `cli`, `api` or `fake`, in any case and with spaces around it ignored, and is `cli` when not set. Any other value SHALL be refused with an error that names it. `weir serve` SHALL also refuse to start with `api`, which is accepted as a name but is not built. `WEIR_AGENT_RUNNER` MUST be `inline`, `docker`, `kubernetes` or `cloudrun`, and `serve` refuses any other value, or one of the last three without the settings it needs.

#### Scenario: A runner that does not exist
- **WHEN** Weir is started with `WEIR_RUNNER=magic`
- **THEN** it fails with an error that names `magic`

#### Scenario: A runner that is not built
- **WHEN** `weir serve` is started with `WEIR_RUNNER=api`
- **THEN** it refuses to start and says the runner is not built yet

### Requirement: Lists and pairs are read from single settings
A setting that holds a list, such as `WEIR_AGENT_ENV`, SHALL be read as names separated by commas, with spaces trimmed and empty items dropped. A setting that holds pairs, such as `WEIR_CLAUDE_MODELS`, SHALL be read as `key=value` items separated by commas. An item that is not a pair, or has an empty key or value, is ignored.

#### Scenario: A list with gaps
- **WHEN** a list setting is ` A, ,B ,`
- **THEN** it is read as `A` and `B`

#### Scenario: Pairs with a broken item
- **WHEN** a pairs setting is `small=claude-haiku-5-5, large = claude-opus-5-5, broken, =x, y=`
- **THEN** it is read as `small` and `large` only

### Requirement: Migrating creates the schema and the default workspace
`weir migrate` SHALL bring the database up to the latest schema. A newly migrated database holds one workspace, `default`. Migrating a database that is already up to date SHALL change nothing. The server does not migrate when it starts: `weir serve` on a database that has not been migrated MUST fail and ask whether the schema has been migrated.

#### Scenario: Migrating twice
- **WHEN** a database is migrated, and then migrated again
- **THEN** both end on the same schema version
- **AND** the database holds the one workspace `default`

#### Scenario: A server on an empty database
- **WHEN** `weir serve` is started on a database that was never migrated
- **THEN** it fails, and the error asks whether the schema has been migrated

### Requirement: Migrations started together take turns
Several migrations started at once on one database SHALL each wait for the one before, so that all of them succeed and end on the same schema version. A migration waits up to five minutes for its turn.

#### Scenario: Five servers start together
- **WHEN** five migrations are started at the same moment on an empty database
- **THEN** all five succeed
- **AND** all five report the same schema version

### Requirement: There is an image for the server and one for agents
The repository SHALL build two container images: one for the server (`deploy/Dockerfile`) and one for agents (`deploy/agent.Dockerfile`). Each runs as a user that is not root. The server's image MUST NOT start without `WEIR_ADMIN_KEY`. Both are published to `ghcr.io`, as `weir` and `weir-agent`.

#### Scenario: What the server's image holds
- **WHEN** a container is started from the server's image with no command
- **THEN** it runs `weir serve`, with the console inside and served, and `git` installed

#### Scenario: The server's image is started with no admin key
- **WHEN** a container is started from the server's image with a database and no `WEIR_ADMIN_KEY`
- **THEN** the server refuses to start, because the image sets `WEIR_REQUIRE_ADMIN_KEY`

#### Scenario: What the agents' image holds
- **WHEN** a container is started from the agents' image with no command
- **THEN** it runs `weir agent`, with the Claude command line and `git` installed

#### Scenario: A push to main, or a version tag
- **WHEN** a commit is pushed to `main`
- **THEN** both images are built for `amd64` and `arm64` and published with the tag `edge`
- **AND** a version tag publishes them under that version

#### Scenario: A pull request changes an image
- **WHEN** a pull request changes a file under `deploy/`
- **THEN** both images are built, and neither is published

### Requirement: Docker Compose starts Postgres, and Weir when asked
`docker compose up` SHALL start Postgres alone, reachable on port 54329 of the host. `docker compose --profile weir up` SHALL also build the server's image from the checkout, migrate the database, and then start Weir on port 8080 with the admin key `local-admin-key`. Weir is not started until the migration has finished successfully.

#### Scenario: Weir is started with compose
- **WHEN** `docker compose --profile weir up` is run in a checkout
- **THEN** `GET http://localhost:8080/v1/workspaces` with `Authorization: Bearer local-admin-key` is answered

### Requirement: The Helm chart installs the server on Kubernetes
The chart in `deploy/helm/weir` SHALL make a Deployment of the server, a Service and a ServiceAccount, and SHALL run `weir migrate` for each release. It holds no secret: `existingSecret` names a Secret whose keys become the server's environment. The chart runs no database.

#### Scenario: A release is upgraded
- **WHEN** `helm upgrade` is run with the default values, where `migrate.when` is `before`
- **THEN** a Job runs `weir migrate` before each install and upgrade, before the new server starts
- **AND** a migration that fails stops the upgrade

#### Scenario: Migrating when the server starts
- **WHEN** the chart is installed with `migrate.when: start`
- **THEN** `weir migrate` runs in the server's pod before the server starts

#### Scenario: Agents run as Jobs
- **WHEN** the chart is installed with `agents.runner: kubernetes`
- **THEN** the server is set to start each agent as a Job
- **AND** the server's account may create, read and delete Jobs, and read pods and their logs, in the namespace agents run in, and nothing else

#### Scenario: Agents run beside the worker
- **WHEN** the chart is installed with `agents.runner: inline`
- **THEN** no Role or RoleBinding is made

#### Scenario: Health checks and the way in
- **WHEN** the chart is installed
- **THEN** the server's pod is checked on `/healthz`
- **AND** an Ingress is made only with `ingress.enabled`

### Requirement: A server can answer the API without doing the work
With `WEIR_WORKER=off` a server SHALL answer the API, serve streams and the console, and start no worker, leaving the work to another server on the same database. With `on`, or with the setting left out, the server does the work as well.

#### Scenario: Two servers on one database
- **WHEN** one server runs with `WEIR_WORKER=off` and another without the setting, on the same database
- **THEN** a run started through the first is worked by the second
