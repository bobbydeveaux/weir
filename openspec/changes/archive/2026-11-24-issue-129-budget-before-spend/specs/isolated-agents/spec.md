## MODIFIED Requirements

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
