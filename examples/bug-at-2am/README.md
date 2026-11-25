# A bug at 2am

A release breaks checkout while everyone is asleep. By the time the on-call engineer picks up their phone, a fix is waiting for a yes or a no.

It uses all five step kinds, and starts from an event.

| Step | Kind | What it does |
| --- | --- | --- |
| The trigger | a rule | Starts a run for an `errors.new` event whose severity is `error`. A warning starts nothing, and no model is called |
| `route` | decide | Labels the error `fix` or `needs_human`. If the model is less than 80% sure, a person gets it |
| `open_issue` | deterministic | Opens an issue. Here it only echoes what it would open |
| `fix` | ai | An agent writes the fix on a branch, inside a $15 budget |
| `approve` | human | The on-call engineer approves or rejects. The run is parked until then |
| `deploy` | deterministic | Merges and deploys. Here it only echoes. The agent never held the credentials |
| `verify` | external | Asks the error tracker whether the error has stopped, and waits for its answer |
| `page` | human | Where an error that needs a person, or a rejected fix, ends up |

## What is proven, and what is not

`engine/examples_test.go` runs this file as it is written, with a scripted model and a stand-in error tracker:

- a warning starts no run and calls no model;
- an error is fixed, approved, deployed and confirmed, and the error tracker is not called before the approval;
- an unsure classifier sends the error to a person, and no agent runs;
- a rejected fix goes to a person and never reaches the error tracker.

It has not been run with a real model or a real error tracker. The agent has no tools in this file, so with a real model it can only describe a fix. To let it work on a repository, give the step a working directory and the GitHub tools: see "An agent that works in a prepared directory" and "GitHub tools" in the main README.

## Run it

You need Postgres (`make up`), a server (`./bin/weir serve`), and a service of your own at the `errors` connection that accepts `POST /verify` and later returns the callback token it was sent.

```sh
./bin/weir connection errors https://errors.example.com
./bin/weir template incident-route --as ops \
  --system 'You decide whether a production error is a small fault in the code that an agent could fix.' \
  --user 'Service {{service}} reported: {{message}}'
./bin/weir template incident-fix --as ops \
  --system 'You fix one fault in one service with the smallest change that works.' \
  --user 'Service {{service}} reported: {{message}}. Fix it on a new branch and say what you changed.'
./bin/weir publish examples/bug-at-2am/workflow.yaml --as ops

curl -X POST "$WEIR_URL/v1/events" -H "Authorization: Bearer $WEIR_API_KEY" -H 'Content-Type: application/json' -d '{
  "type": "errors.new",
  "id": "E-7-first-seen",
  "data": {"id": "E-7", "severity": "error", "service": "checkout", "message": "nil basket in Checkout.total"}
}'

./bin/weir tasks
./bin/weir approve <task-id> --as oncall
```

The reply to the event lists the run it started. After the approval the run waits at `verify` until your service posts to the callback address it was given.
