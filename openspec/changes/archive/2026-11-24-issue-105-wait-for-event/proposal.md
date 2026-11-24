## Why

An event can start a run. It cannot move one that is already waiting. A workflow that opens a pull request has no way to wait for GitHub to say it was merged: today a person clicks to say so, and nothing checks. GitHub already tells Weir, through a signed webhook, the moment it happens.

Closes issue 105.

## What Changes

- An `external` step can set `resume: event`, with the `event` type it waits for and a `when` rule that says which event is this run's.
- The event that matches completes the step and becomes its output.
- A step that starts waiting looks back over events received since its run started, so an event that arrives first is not missed.
- One event can start workflows through triggers and also move waiting steps on. The reply to an event, the list of events and the webhook reply say what it resumed.
- A step that only waits for an event names no connection and makes no call.

Out of scope: moving waiting steps on after a crash between an event being recorded and acted on; a separate way for a person to stand in for the sender (posting the event by hand already does it).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `external-steps`: a third resume mode, `event`, and a step that makes no call.
- `events-and-triggers`: an event also moves on the steps that were waiting for it.

## Impact

- `spec/`: two new step fields, `event` and `when`, and their rules at publishing.
- `engine/`: a new table, `event_waits`; a `resumed` column on `inbound_events`; parking and event intake.
- `worker/`: an external step with no call parks at once.
- `api/` and the console: what an event resumed is reported.
