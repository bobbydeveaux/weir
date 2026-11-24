# events-and-triggers Specification

## Purpose
An application tells Weir what happened, and Weir starts the workflows that want to know. A workflow says what starts it with triggers: an event, or a schedule.

## Requirements

### Requirement: An application reports an event
`POST /v1/events` SHALL take an event with a `type` and, optionally, an `id`, a `source` and `data`. Weir SHALL record the event in the caller's workspace and reply 202 with its own `id` for the event, `duplicate`, and `runs`, the list of what the event started. An event with no `type`, or with data that is not JSON, SHALL be refused. An event that no workflow wants SHALL still be recorded, and that is not an error.

#### Scenario: An event nothing is waiting for
- **WHEN** an application sends `{"type": "file.renamed"}` and no workflow has a trigger for it
- **THEN** the reply is 202 with an empty list of runs
- **AND** the event is in the list of events

### Requirement: A repeated event starts nothing new
When an event arrives with the same `type` and `id` as one already received in that workspace, Weir SHALL start nothing and resume nothing, and SHALL reply with `duplicate: true`, the runs the event started the first time, and the steps it resumed the first time. An event sent with no `id` cannot be recognised as a repeat.

#### Scenario: The same event twice
- **WHEN** an application sends `file.uploaded` with the id `f-1`, and then sends it again
- **THEN** the second reply holds `duplicate: true` and the same run ids as the first
- **AND** no new run exists

#### Scenario: A repeat of an event a step took
- **WHEN** an event that moved a waiting step on is sent again with the same id
- **THEN** the reply holds `duplicate: true` and the same step under `resumed`
- **AND** nothing else changes

### Requirement: A trigger names the event that starts a workflow
A definition MAY list `triggers:`, each with an `event` type. Weir SHALL match events against the triggers of each workflow's live version only. Publishing SHALL refuse a trigger whose event type is not lower-case words joined by dots, hyphens or underscores, and one that has both an `event` and a `schedule`.

#### Scenario: A malformed event type
- **WHEN** a definition with the trigger `event: Not Valid` is published
- **THEN** publishing is refused

### Requirement: An event starts every workflow whose trigger matches
For each workflow in the workspace whose live version has a matching trigger, Weir SHALL start one run, with the event's sender as its requester. The reply SHALL name each workflow by `namespace` and `name`, with the `run_id` it started or an `error` saying why it could not start. A workflow in another workspace MUST NOT hear the event.

#### Scenario: Two workflows want the same event
- **WHEN** `archive` and `read-pdf` both have a trigger for `file.uploaded`, and one such event arrives
- **THEN** the reply lists a run for each of them

#### Scenario: Another workspace wants it too
- **WHEN** a workflow in the workspace `rival` has a trigger for `file.uploaded`, and the event is sent to the default workspace
- **THEN** `rival` has no runs

### Requirement: A trigger can filter events and shape the run's input
A trigger MAY set `when`, an expression that sees the event as `event` (its `type`, `id`, `source` and `data`). The trigger SHALL match only when it is true. A trigger MAY set `input`, a mapping that builds the run's input from the event. Without it, the run's input SHALL be the event's data. The first trigger in a workflow that matches decides the input.

#### Scenario: The condition is false
- **WHEN** a trigger has `when: event.data.type == "pdf"` and the event's data has `type: txt`
- **THEN** that workflow is not started

#### Scenario: The input is mapped
- **WHEN** a trigger has `input: { document: event.data.name, from: event.source }`, and an event from `portal` arrives with the name `treaty.pdf`
- **THEN** the run's input is `{"document": "treaty.pdf", "from": "portal"}`

### Requirement: A schedule trigger starts a workflow by the clock
A trigger MAY give a `schedule` in place of an `event`: a five-field cron expression (minute, hour, day of the month, month, day of the week), read in UTC. Weir SHALL start a run when the schedule comes due, with the requester `schedule`. Publishing SHALL refuse an expression that is not one, and a schedule trigger that sets `when`. Publishing the workflow again without the schedule SHALL stop it.

#### Scenario: A schedule that is not one
- **WHEN** a definition with the trigger `schedule: every morning` is published
- **THEN** publishing is refused

#### Scenario: What a field may hold
- **WHEN** a schedule's fields are each `*`, a number, a range such as `1-5`, a list such as `5,35` or a step such as `*/15`
- **THEN** the schedule is accepted

#### Scenario: The next time is worked out in UTC
- **WHEN** the schedule is `0 7 * * 1-5` and the time is 08:00 UTC on a Friday
- **THEN** it is next due at 07:00 UTC on the Monday

#### Scenario: A scheduled run's input
- **WHEN** a schedule trigger sets `input: { due: event.data.at }` and comes due
- **THEN** the run's input holds the time the schedule was due
- **AND** a schedule trigger with no `input` starts its run with the input `{}`

#### Scenario: The schedule is taken away
- **WHEN** a workflow with a schedule is published again with only an event trigger
- **THEN** no schedule remains for it

### Requirement: A due schedule starts exactly one run
When a schedule's time has come, Weir SHALL start one run of the workflow in the workflow's own workspace, however many workers are looking at once, and SHALL move the schedule on to its next time. A schedule that is not yet due SHALL start nothing.

#### Scenario: Four workers look at once
- **WHEN** a schedule is due and four sweeps run at the same moment
- **THEN** exactly one run exists, with the requester `schedule`
- **AND** the schedule is next due at a time in the future

### Requirement: A missed schedule fires once
When a schedule fell due while Weir was not running, Weir SHALL fire it once when it is back, and SHALL then wait for the next time after now. It MUST NOT start a run for every time it missed.

#### Scenario: Weir was away
- **WHEN** a schedule's due time is in the past when Weir next looks
- **THEN** one run is started and the schedule's next time is after now

### Requirement: Events and triggers can be listed
`GET /v1/events` SHALL list the workspace's most recent events, newest first, each with its type, source, sender's id, data, who sent it, when it arrived, the runs it started and the steps it resumed. It returns 50 unless `limit` asks for another number up to 200. `GET /v1/triggers` SHALL list every trigger of the workspace's live versions, each with its workflow and version, its `event` or its `schedule`, its `when`, and for a schedule the time it is next due (`next_at`).

#### Scenario: Three events were sent
- **WHEN** three events have been sent and the events are listed
- **THEN** the list holds three, with the last one sent first

#### Scenario: A schedule is listed
- **WHEN** a workflow with the schedule `0 7 * * 1-5` is published and the triggers are listed
- **THEN** the list holds that schedule with a `next_at`

#### Scenario: An event that moved a step on
- **WHEN** an event moved a waiting step on and the events are listed
- **THEN** that event is listed with the run and the step under `resumed`

### Requirement: An event moves on the steps that were waiting for it
Beside starting workflows through triggers, an event SHALL be given to every step in its workspace that is waiting for its type, and each step whose rule is true SHALL take it. Starting and resuming are separate: one event MAY do both. The reply to `POST /v1/events` SHALL list each step moved on under `resumed`, with its run. A step whose rule could not be worked out for the event SHALL be listed with the reason, and SHALL still be waiting.

#### Scenario: An event a run was waiting for
- **WHEN** a run waits for `github.pull_request` and the event that is about it arrives
- **THEN** the reply lists that run and step under `resumed`

#### Scenario: An event nothing was waiting for
- **WHEN** an event arrives that no step is waiting for
- **THEN** `resumed` is empty, and the event still starts whatever its triggers say
