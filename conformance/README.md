# The conformance suite

Each folder under `cases/` is one case: a workflow, what goes in, what the world outside Weir does, and the event log the run must produce. The cases are data, so another implementation of the contract can run the same ones.

```sh
make test-db                       # runs them with every other test
go test ./conformance              # or by themselves; needs WEIR_TEST_DATABASE_URL
```

## A case

| File | What it holds |
| --- | --- |
| `workflow.yaml` | The definition, published as it is written |
| `case.yaml` | The inputs, the scripted model, the third parties, what is done to the run from outside, and how it must end |
| `events.txt` | The event log the run must produce: one event to a line, as its type and then the step it is about |

A case uses only the built-in handlers `core.echo` and `core.noop`, so it needs no code of its own.

### `case.yaml`

```yaml
description: One sentence saying what the case proves.
templates: [route, fix]            # templates to publish first; their text does not matter
connections:                       # stand-in third parties, each with its replies in turn
  scanner:
    - { status: pending }          # the last reply is repeated
    - { status: done }
model:                             # the scripted model
  cost_usd: 3.20                   # charged for each call an ai step makes
  decisions:
    route: { label: fix, confidence: 0.95 }
  outputs:
    fix: [{ branch: fix/482 }]     # one for each call in turn; the last is repeated
steps:                             # what is done from outside, in order
  - start: { requester: alice, input: { title: Checkout is down } }
  - emit: { type: order.paid, id: e-4, data: { id: A-2 } }
  - decide: { as: bob, outcome: approved }
  - decide: { as: alice, outcome: approved, refused: true }   # must be refused
  - callback: { connection: detector, output: { cleared: true } }
  - cancel: { as: bob, reason: no longer needed }
expect:
  status: succeeded                # the run's status at the end
  error: budget_exceeded           # the type of the run's error; left out when there is none
```

After each entry under `steps` the runner lets the worker do everything it can, then moves to the next. The run that is checked is the one `start` made, or the first one an `emit` started.

A field the runner does not know is an error, so a misspelt one cannot pass unnoticed.

## The cases

| Case | What it proves |
| --- | --- |
| `all-five-kinds` | One run through a decide, a deterministic, an ai, a human and an external step |
| `requester-cannot-approve` | The requester is refused; a refusal writes no event; a rejection goes where the workflow sends it |
| `unsure-decision-takes-the-default` | A classifier below the step's confidence gets the default label, and no agent runs |
| `poll-until-done` | A poll step asks until its condition holds |
| `budget-spent` | The step that takes a run past its budget fails, and the run is blocked |
| `output-fails-its-schema` | Output that does not match its schema is refused and the step tried again |
| `event-starts-and-resumes` | An event starts a run by a trigger, and a waiting step takes only the event about its run |
| `cancelled-while-waiting` | A cancelled run stops where it is |

## Adding or changing a case

Write `workflow.yaml` and `case.yaml`, then record the log:

```sh
WEIR_CONFORMANCE_RECORD=1 go test ./conformance -run 'TestConformance/your-case'
```

Read `events.txt` before you commit it. It is the contract: recording a mistake makes the mistake the contract. A change to an existing `events.txt` is a change to what Weir promises, and the pull request must say why.

## What it does not cover yet

- Only the type and the step of each event are compared. The actor and the data are not.
- The suite runs inside Weir's own test process, against the engine. It does not yet run against a server over HTTP, which is what another implementation would need.
- Not covered by a case: a crashed worker, an expired lease, a repeated completion, compensation, hooks, schedules, files, tools and isolated agents. Tests elsewhere cover them.
