## MODIFIED Requirements

### Requirement: An external step calls a named connection
An external step SHALL name a `connection` and a `call`, unless it only waits for an event. When the step runs, Weir SHALL send one HTTP request to the connection's address followed by `call.path`, with the method in `call.method`, or `POST` when none is given, and a JSON body. The connection's credential SHALL be added, and without it the call SHALL NOT be made. Publishing SHALL refuse a step that names a connection the workspace does not have.

#### Scenario: A step calls a third party
- **WHEN** a run reaches an external step for the connection `detector` with `call: { path: /verify }`
- **THEN** the third party receives one request at `/verify`

#### Scenario: The connection's secret is not set
- **WHEN** an external step runs and its connection's secret cannot be found
- **THEN** no request is sent
- **AND** the step fails with the error type `secret_unavailable`

#### Scenario: A step that calls names both a connection and a call
- **WHEN** a definition is published whose external step has `resume: callback` and no connection, or a connection and no call
- **THEN** publishing is refused

### Requirement: A resume mode is required
Publishing SHALL refuse an external step whose `resume` is not `callback`, `poll` or `event`. It SHALL refuse a poll step with no `poll.until`, a step that sets `poll` without `resume: poll`, and a step that sets `event` or `when` without `resume: event`.

#### Scenario: A poll step with no condition
- **WHEN** a definition whose external step has `resume: poll` and no `poll.until` is published
- **THEN** publishing is refused

#### Scenario: A rule on a callback step
- **WHEN** a definition whose external step has `resume: callback` and a `when` is published
- **THEN** publishing is refused, and the error says `when` is only allowed with `resume: event`

## ADDED Requirements

### Requirement: A step can wait for an event that is about its run
An external step with `resume: event` SHALL wait for an event of the type it names in `event`, in its own workspace, for which its `when` rule is true. The rule SHALL see the event as `event`, and the run's own `input` and `steps`. While the step waits, the run's status SHALL be `waiting`. An event for which the rule is false, an event of another type, and an event in another workspace SHALL NOT move the step.

#### Scenario: Two runs wait for their own pull requests
- **WHEN** two runs wait for `github.pull_request` with `when: event.data.number == input.number`, and an event arrives with the first run's number
- **THEN** the first run moves on
- **AND** the second run is still `waiting`

#### Scenario: The same event in another workspace
- **WHEN** a run waits, and a matching event is sent in another workspace
- **THEN** the run is still `waiting`

### Requirement: The event a step takes becomes its output
When a waiting step takes an event, the step SHALL succeed, and its output SHALL hold the event's `type`, `id`, `source`, `sent_by` and `data`. Whoever sent the event SHALL be recorded as the actor who completed the step. The step's `next` rules SHALL be able to read the output, so that a run can go different ways for different events.

#### Scenario: A pull request closed without being merged
- **WHEN** a step waits for a pull request to be closed, with a rule `when: "!output.data.merged"` that goes to `abandoned`, and the closing event says it was not merged
- **THEN** the run goes to `abandoned`

#### Scenario: A signed webhook is the sender
- **WHEN** a step takes an event that arrived as a webhook on the connection `github`
- **THEN** the step's output holds `sent_by: webhook:github`

### Requirement: A step that waits for an event must say which, and for how long
Publishing SHALL refuse a step with `resume: event` that has no `event`, no `when` or no `timeout`. When the timeout passes with no event taken, the step SHALL fail with the error type `timeout`, and an event that arrives afterwards SHALL NOT move it.

#### Scenario: No rule
- **WHEN** a definition is published whose step has `resume: event` and no `when`
- **THEN** publishing is refused

#### Scenario: The event never comes
- **WHEN** a step with the default failure policy waits past its timeout
- **THEN** the run is `blocked` with the error type `timeout`
- **AND** a matching event sent afterwards resumes nothing

### Requirement: An event that arrives before the step starts waiting is not missed
When a step starts waiting, Weir SHALL look back over the events of its type received since its run started, and take the earliest one its rule is true for. It SHALL consider at most the latest 1,000. An event received before the run started SHALL NOT be taken.

#### Scenario: The event comes first
- **WHEN** a run has started, a matching event arrives before any worker has reached the waiting step, and a worker then reaches it
- **THEN** the step takes that event and the run moves on

#### Scenario: An event from before the run
- **WHEN** a matching event arrived before the run was started
- **THEN** the step waits

### Requirement: A run takes any one event once
An event that a step of a run has taken SHALL NOT be taken again by a step of the same run.

#### Scenario: Two steps wait for the same kind of event
- **WHEN** a run has two steps in turn that wait for `review.approved` with the same rule, and one such event arrives
- **THEN** the first step takes it
- **AND** the second step waits for another

### Requirement: A step may call a third party and then wait for its event
A step with `resume: event` MAY also name a `connection` and a `call`. Weir SHALL then make the call once, without a callback token, and wait for the event.

#### Scenario: A scanner that reports back with a webhook
- **WHEN** a step calls `/scans` on a connection and waits for `scanner.finished`
- **THEN** the third party receives one request, with no callback token
- **AND** the run moves on when the event arrives
