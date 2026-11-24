## MODIFIED Requirements

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

## ADDED Requirements

### Requirement: An event moves on the steps that were waiting for it
Beside starting workflows through triggers, an event SHALL be given to every step in its workspace that is waiting for its type, and each step whose rule is true SHALL take it. Starting and resuming are separate: one event MAY do both. The reply to `POST /v1/events` SHALL list each step moved on under `resumed`, with its run. A step whose rule could not be worked out for the event SHALL be listed with the reason, and SHALL still be waiting.

#### Scenario: An event a run was waiting for
- **WHEN** a run waits for `github.pull_request` and the event that is about it arrives
- **THEN** the reply lists that run and step under `resumed`

#### Scenario: An event nothing was waiting for
- **WHEN** an event arrives that no step is waiting for
- **THEN** `resumed` is empty, and the event still starts whatever its triggers say
