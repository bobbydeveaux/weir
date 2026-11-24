# console Specification

## Purpose
The console is the web interface in `console/`. It shows a workspace's workflows, runs, tasks, events, templates and connections, and lets a person start, resume and cancel runs and decide tasks. It does everything through the HTTP API.

## Requirements

### Requirement: The console is a static page that talks only to the API
The console SHALL be built as static files with `index.html` as the page for every address. It SHALL read and change Weir only through the HTTP API, at `/v1` on its own origin unless the build was given another base in `VITE_WEIR_API_BASE`. When the API cannot be reached it SHALL show a banner naming the address it tried, and go on trying.

#### Scenario: The API is down
- **WHEN** a request from the console gets no answer, or a gateway error
- **THEN** a banner says the API cannot be reached and names the address
- **AND** the banner goes when the API next answers

### Requirement: A person signs in by pasting an API key
The console SHALL offer a field for an API key and send the key with every request. It keeps the key for the browser tab only. It SHALL also offer an "Acting as" field for the person's name, which it sends as the actor on every change, and it MUST refuse to send a change while that name is empty. Behind a sign-in proxy it SHALL show who is signed in, and show neither field.

#### Scenario: A server that needs a key
- **WHEN** the console is opened against a server that answers 401, and no key has been pasted
- **THEN** the key field is marked, and says the server needs a key

#### Scenario: The tab is closed
- **WHEN** a person pastes a key, closes the tab and opens the console again
- **THEN** the key is gone and must be pasted again

#### Scenario: A change with no name
- **WHEN** a person presses "Start run" with "Acting as" empty
- **THEN** nothing is sent, and the console says to set the name first

#### Scenario: Behind a sign-in proxy
- **WHEN** the server says the caller was signed in by a proxy
- **THEN** the console shows "Signed in as" and the person's address, and uses it as the actor

### Requirement: A person chooses the workspace
The console SHALL list the workspaces the caller can see and work in the one chosen, which it remembers in the browser. The "Connections & registry" page SHALL list the workspaces and offer a form to create one from a slug and an optional name, then offer to switch to it.

#### Scenario: A link names a workspace
- **WHEN** the console is opened at an address with `?workspace=acme`
- **THEN** it works in `acme`, and remembers that choice

#### Scenario: The remembered workspace is gone
- **WHEN** the workspace the browser remembers no longer exists
- **THEN** the console moves to `default`, or to the first one listed

#### Scenario: An empty workspace
- **WHEN** the chosen workspace has no workflows and `acme` has three
- **THEN** the console says so and offers a button to switch to `acme`

#### Scenario: A workspace is created
- **WHEN** a person enters the slug `acme` and presses "Create workspace"
- **THEN** the workspace is created and appears in the workspace chooser

### Requirement: The overview shows what ran and what it cost
The overview page SHALL show, for the last 7, 30 or 90 days: the number of runs, how many are active, blocked, succeeded and failed, the spend, the number of model calls and their tokens, spend per day as a chart, and tables of runs and spend by workflow and of calls, tokens, cost, time and errors by model. It SHALL also show the first three open human tasks, which can be decided there.

#### Scenario: A range is chosen
- **WHEN** a person presses "7 days"
- **THEN** the tiles, the chart and the tables show the last seven days

### Requirement: Workflows can be browsed and started
The workflows page SHALL list every workflow in the workspace at its live version, and filter the list by name. A workflow's page SHALL show its steps as a diagram, its YAML source as published, its versions and its recent runs, and SHALL offer "Start run". The console does not publish or edit workflows.

#### Scenario: The list of workflows
- **WHEN** the workflows page is opened
- **THEN** each workflow shows its steps, its counts of runs and its spend

#### Scenario: A workflow is looked at
- **WHEN** a person opens a workflow and picks a step in the diagram
- **THEN** the step's details are shown
- **AND** the versions tab says who published each version and when

#### Scenario: A run is started
- **WHEN** a person presses "Start run", fills in the input as JSON, the alias to run and an optional idempotency key, and submits
- **THEN** the run is started and the console opens its page

#### Scenario: Input that is not JSON
- **WHEN** the input is not valid JSON
- **THEN** the form says so and the button cannot be pressed

### Requirement: Runs can be listed and followed
The runs page SHALL list runs newest first, filtered by workflow and by status, and refresh when any run in the workspace moves. A run's page SHALL show the run's status, steps, timeline, input, output, files and event log, and SHALL follow the run as it happens.

#### Scenario: How many runs are listed
- **WHEN** a person chooses how many runs to show
- **THEN** the choices are 50, 100 and 200

#### Scenario: A run moves while it is on screen
- **WHEN** a step of the run on screen finishes
- **THEN** the page shows the new state without being reloaded

#### Scenario: A step is looked at
- **WHEN** a person picks a step on a run's page
- **THEN** the page shows what the step took in and what it gave back

#### Scenario: A file is downloaded
- **WHEN** a person presses "Download" beside a file a step wrote
- **THEN** the browser saves the file

#### Scenario: A run from another workspace
- **WHEN** a run's page is opened in a workspace the run is not in
- **THEN** the run is shown as not found, with a note that runs belong to one workspace

### Requirement: A blocked run can be resumed or cancelled
On the page of a run that is `blocked`, the console SHALL offer "Resume run", with an optional new budget in USD, and "Cancel run", which asks for a reason first and does nothing if the person dismisses the question. It offers neither for a run in any other status.

#### Scenario: A blocked run is resumed with more budget
- **WHEN** a person enters a new budget and presses "Resume run"
- **THEN** the run is resumed with that budget

### Requirement: Tasks can be seen and decided
The tasks page SHALL list the human steps waiting for a decision, oldest first. A person decides by pressing one of the task's outcomes, with an optional note, and a refusal from the server SHALL be shown on the task.

#### Scenario: What a task shows
- **WHEN** a task is on screen
- **THEN** it shows its workflow and step, who it is for, how long it has waited and whether it was escalated
- **AND** it shows what the step was given, in a readable form or as JSON

#### Scenario: A task is approved
- **WHEN** a person presses "approved" on a task
- **THEN** the decision is sent with their name, and the task says it was recorded

#### Scenario: The requester tries to approve
- **WHEN** the server refuses a decision
- **THEN** the task shows the server's reason and stays open

#### Scenario: Tasks for more than one role
- **WHEN** the open tasks are for more than one role
- **THEN** the page offers a filter by role

#### Scenario: Tasks elsewhere in the console
- **WHEN** tasks are open
- **THEN** the navigation shows how many, and a run's own page shows the task that run is waiting on

### Requirement: Events, templates and connections can be read
The events page SHALL show the workspace's triggers, schedules and the events it has received. The templates page SHALL show each template and its versions. The "Connections & registry" page SHALL show the workspace's connections and what the server offers. The console does not create or change templates or connections.

#### Scenario: The events page
- **WHEN** the events page is opened
- **THEN** it shows which events start which workflows and on what condition, each schedule with when it is next due and last ran, and each event received with the runs it started

#### Scenario: An event started nothing
- **WHEN** an event was received that no trigger matched
- **THEN** its row says nothing started

#### Scenario: A template is looked at
- **WHEN** a person picks a template and one of its versions
- **THEN** the page shows who published that version, and its system and user prompts

#### Scenario: The connections and registry page
- **WHEN** the page is opened
- **THEN** each connection shows its kind, address, tools and the name of the secret it signs in with
- **AND** the page lists the handlers, decide providers and tools the server offers

### Requirement: The server can serve the console itself
When `WEIR_CONSOLE_DIR` names a directory of the console's built files, the server SHALL serve them at every address that is not the API's. A request that reads a file in the directory gets that file, and any other address gets `index.html`. `/healthz` and everything under `/v1` MUST still be answered by the API, and a path outside the directory MUST NOT be served. The server's container image is built this way. A server without the setting serves no console.

#### Scenario: A page of the console is opened
- **WHEN** `GET /runs/some-run` is requested from a server with a console directory
- **THEN** the reply is the console's `index.html`

#### Scenario: An API address that does not exist
- **WHEN** `GET /v1/no-such-thing` is requested from the same server
- **THEN** the reply is the API's 404, not the console
