# isolated-agents Specification

## Purpose
An agent that does real work can run in a container of its own, with every tool and nothing in reach worth stealing. The container talks only to Weir's API, with the one-time token for its step.

## Requirements

### Requirement: A setting sends every agent to a container of its own
`WEIR_AGENT_RUNNER` SHALL say where agents run: `inline` (the default, beside the worker), `docker`, `kubernetes` or `cloudrun`. With any of the last three, the agent of every `ai` step SHALL run in a container of its own, one per attempt, and the worker's own runner MUST NOT be used for it. The server SHALL refuse to start with any other value, or without the settings its choice needs.

#### Scenario: A step runs with a launcher set
- **WHEN** a worker with a launcher runs an `ai` step
- **THEN** a container is started for the step and the worker's own runner is never called

#### Scenario: A setting the choice needs is missing
- **WHEN** the server is started with `docker` and no `WEIR_AGENT_IMAGE`, with `kubernetes` and no `WEIR_AGENT_IMAGE` or no `WEIR_AGENT_URL`, or with `cloudrun` and no `WEIR_AGENT_JOB` or no `WEIR_AGENT_URL`
- **THEN** it refuses to start and names the setting

### Requirement: The container is given Weir's address and the token for its step
Weir SHALL start the container with two things of its own: `WEIR_URL`, its address as the container reaches it, and `WEIR_AGENT_TOKEN`, the one-time token of the attempt. It MUST NOT give the container an API key, the database's address, or any third party's credential. The token alone SHALL open the agent's part of the API, on a server that demands an API key everywhere else.

#### Scenario: A server with keys on
- **WHEN** the API demands a key, and a container holds only its step's token
- **THEN** the container fetches its job and sends its result
- **AND** a request for the run's files with no key gets 401

### Requirement: The agent fetches its job and its directory from the API
The container SHALL fetch its job with `GET /v1/agent/{token}/job`. When the step has a prepared working directory or documents, the worker SHALL prepare them first, with whatever credential that takes, and the container SHALL fetch them as an archive with `GET /v1/agent/{token}/workspace`. The credential used to prepare the directory MUST NOT reach the container.

#### Scenario: What a job holds
- **WHEN** a container fetches its job
- **THEN** it gets the prompt, the model Weir chose, the output schema, the most its call may spend, the tools the step lists and the limits on what it may write

#### Scenario: The job carries what the run has left
- **WHEN** a step with a budget of 1 dollar runs in a container, in a run that has 0.48 dollars left
- **THEN** the agent in the container is told to stop at 0.48 dollars

#### Scenario: A prepared directory reaches the agent
- **WHEN** a step's workspace handler writes `main.go` on the worker
- **THEN** the agent in the container finds `main.go` in its working directory

#### Scenario: A directory too large to send
- **WHEN** a step's directory packs to more than 30 MB
- **THEN** the step fails

### Requirement: The agent in a container is given every tool
Inside the container the agent SHALL run with every built-in tool and without a sandbox or permission prompts: the container is the boundary. Unless `WEIR_AGENT_PERMISSIONS` says otherwise, permission checks SHALL be off. It SHALL still load no user or project settings from the directory it works in, and no MCP servers but Weir's. It SHALL be given the container's environment, apart from variables that begin `WEIR_`.

#### Scenario: The command line in a container
- **WHEN** an agent is started in a container
- **THEN** its command line is not restricted to a list of tools and has no sandbox
- **AND** it is still told to ignore user and project settings and to use only the MCP configuration Weir gives it

#### Scenario: The setting is left out
- **WHEN** an agent is started in a container with no `WEIR_AGENT_PERMISSIONS`, or with `bypass`
- **THEN** its command line switches permission checks off, as before the setting existed

### Requirement: The agent sends one result
The container SHALL send its result with `POST /v1/agent/{token}/result`: the output, what the model call cost, the files it changed, or the reason it failed. Weir SHALL accept one result for an attempt and refuse a second with 409. A result larger than 30 MB SHALL be refused. When a container ends without sending a result, the step SHALL fail and say so. When the agent reports a failure, the step SHALL fail with the agent's reason.

#### Scenario: The container dies
- **WHEN** a container exits without sending anything
- **THEN** the step fails with a message that the container ended without sending a result

#### Scenario: Two results
- **WHEN** a container sends a second result for the same attempt
- **THEN** the second is refused as already sent

### Requirement: Weir checks again everything a container sends
What comes back SHALL be treated as the agent's word. Weir SHALL apply the step's limits to it again: it SHALL fail the step when the result holds more files or more bytes than the step allows, a path that leaves the working directory, files from a step that may write none, or no output. The output, the changed paths and the budget are then checked as for any agent, and the model call is recorded with its prompt and cost.

#### Scenario: More files than the step allows
- **WHEN** a result holds thirty files and the step's limit is twenty
- **THEN** the step fails and says so

#### Scenario: A path that climbs out
- **WHEN** a result holds a file at `../../etc/passwd`
- **THEN** the step fails

### Requirement: The token opens nothing once the step ends
The agent's part of the API SHALL answer only while the attempt behind the token is running. After the step ends, the same token SHALL be refused with 409 for the job and for the directory.

#### Scenario: A token from a finished step
- **WHEN** a finished step's token is used to fetch its job
- **THEN** the reply is 409

### Requirement: An agent can be started with Docker
With `WEIR_AGENT_RUNNER=docker`, Weir SHALL start each agent with `docker run` from `WEIR_AGENT_IMAGE`, remove the container when it ends, and pass it the model's credential and the variables in `WEIR_AGENT_ENV` by name, so that no value is on a command line. `WEIR_AGENT_DOCKER_ARGS` SHALL add arguments such as a network or limits. The agent SHALL run as the container's own user and see none of the server's `WEIR_` variables but the two it is given.

#### Scenario: A real container
- **WHEN** a step runs with a real image and a real model credential
- **THEN** the agent changes files in its directory as the container's own user, and the changes are kept as the step's files

### Requirement: An agent can be started as a Kubernetes Job
With `WEIR_AGENT_RUNNER=kubernetes`, Weir SHALL create one Job per attempt in `WEIR_AGENT_NAMESPACE` (its own namespace if left out), wait for it, and remove it and its pod whether it succeeded, failed or ran out of time. The pod SHALL take its environment from the Secret named by `WEIR_AGENT_SECRET`, which Weir never reads, and MUST NOT be given a credential for the cluster. The Job's name MUST NOT hold the token.

#### Scenario: A Job for a step
- **WHEN** an agent is started on Kubernetes
- **THEN** the Job's pod has `WEIR_URL` and `WEIR_AGENT_TOKEN`, and takes the rest of its environment from the named Secret
- **AND** the pod mounts no service account token, does not run as root, cannot gain privileges, and is run once with no retry
- **AND** the Job's deadline is the time the step has left, and the Job is removed when it ends

#### Scenario: The pod's account and size are set
- **WHEN** `WEIR_AGENT_SERVICE_ACCOUNT`, `WEIR_AGENT_CPU` and `WEIR_AGENT_MEMORY` are set
- **THEN** the pod runs as that account, with those amounts as its requests and limits

#### Scenario: The Job fails
- **WHEN** the Job reports that it failed
- **THEN** the error holds the reason and the last lines of the pod's log, and the Job is still removed

### Requirement: An agent can be started as a run of a Cloud Run job
With `WEIR_AGENT_RUNNER=cloudrun`, Weir SHALL start one run of the job named by `WEIR_AGENT_JOB` per attempt. It SHALL override only the run's environment, task count and timeout, and MUST NOT set the image, the service account or the model's credential: those belong to the job. Weir SHALL sign in to Cloud Run as the service account it runs as, with no key. When the step runs out of time or is cancelled, Weir SHALL cancel the run.

#### Scenario: A run for a step
- **WHEN** an agent is started on Cloud Run
- **THEN** the job is run with `WEIR_URL`, `WEIR_AGENT_TOKEN` and a timeout of the time the step has left
- **AND** nothing about its image or account is sent

#### Scenario: The project and region are left out
- **WHEN** `WEIR_AGENT_PROJECT` and `WEIR_AGENT_REGION` are not set
- **THEN** the job is run in Weir's own project and region

#### Scenario: The run fails
- **WHEN** the run reports that it failed
- **THEN** the error holds the reason and the address of the run's log

#### Scenario: The step runs out of time
- **WHEN** the step's time ends while the run is still going
- **THEN** Weir cancels the run

### Requirement: A setting keeps the agent's permission checks on
`WEIR_AGENT_PERMISSIONS`, read in the agent's container, SHALL choose how the agent is given its tools: `bypass` (the default) or `allow`. With `allow` the command line MUST NOT be told to bypass permission checks; it SHALL instead be told to refuse without asking anything not allowed, and SHALL be given every built-in tool and each of the step's tools as allowed, by name. Any other value SHALL stop the agent before it fetches its job, naming the setting.

#### Scenario: An organisation forbids bypassing permission checks
- **WHEN** an agent is started in a container with `WEIR_AGENT_PERMISSIONS=allow`
- **THEN** its command line does not bypass permission checks
- **AND** the shell, reading, editing, the web and skills are allowed by name, with no sandbox and no limit on which tools it has

#### Scenario: The step lists tools of its own
- **WHEN** such an agent's step lists a tool
- **THEN** that tool is allowed by name beside the built-in ones

#### Scenario: A value that is not known
- **WHEN** an agent is started with `WEIR_AGENT_PERMISSIONS=off`
- **THEN** it stops with an error that names the setting, and fetches no job
