# Support inbox

A message arrives in a shop's support inbox. A small model labels it, an agent drafts a reply, a person approves it, and code sends it. Messages labelled `other` are filed without an agent ever running.

It uses four of the five step kinds: `decide`, `ai`, `human` and `deterministic`.

| Step | Kind | What it does |
| --- | --- | --- |
| `classify` | decide | Picks one of `refund`, `bug`, `sales` or `other`. Anything unclear becomes `other` |
| `draft` | ai | Writes a short reply, as structured output, inside a $0.50 budget |
| `approve` | human | A team lead approves or rejects. The person who started the run cannot |
| `send` | deterministic | Sends the reply. Here it only echoes what it would send |
| `file` | deterministic | Where `other` messages and rejected drafts end up |

## Run it

You need Postgres running (`make up` from the repository root) and the Claude command line signed in.

```sh
export WEIR_DATABASE_URL='postgres://weir:weir@localhost:54329/weir?sslmode=disable'
make build && ./bin/weir migrate

# In one terminal: the API and a worker
./bin/weir serve

# In another: publish the two templates and the workflow
./bin/weir template support-classify --as ops \
  --system 'You sort messages sent to an online shop support inbox.' \
  --user 'Message from {{from}}: {{message}}'

./bin/weir template support-reply --as ops \
  --system 'You write short, kind replies for an online shop support team. Never promise a refund; say a colleague will confirm.' \
  --user 'This message was classed as {{kind}}. Write a reply of at most three sentences. Message: {{message}}'

./bin/weir publish examples/support-inbox/workflow.yaml --as ops

# Start a run as alice
./bin/weir run examples/support-inbox --as alice \
  --input '{"from": "sam@example.com", "message": "My kettle arrived cracked. Can I have my money back please?"}'

./bin/weir status <run-id> --events
./bin/weir tasks

# alice started the run, so she cannot approve it. Someone else can.
./bin/weir approve <task-id> --as bob
./bin/weir status <run-id>
```

## What a real run looked like

Run on 16 November 2026 with the `cli` runner:

| Step | Result | Model | Tokens in / out | Cost | Time |
| --- | --- | --- | --- | --- | --- |
| `classify` | `refund` | claude-haiku-5-5 | 1,221 / 120 | $0.0003 | 2.6 s |
| `draft` | A three-sentence reply that did not promise a refund | claude-haiku-5-5 | 1,178 / 124 | $0.0003 | 2.4 s |

The whole run cost $0.0006. Most of each step's time was the command line starting up.

## What a test proves

`engine/examples_test.go` runs this file as it is written, with a scripted model: a refund is drafted, the requester is refused when she approves it, and it is sent once someone else does; a message labelled `other` is filed with no agent and no cost; a rejected draft is filed.
