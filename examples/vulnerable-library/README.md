# A vulnerable library

A scanner reports a critical vulnerability in a library a service uses. An agent works out whether it matters, a person approves the plan, a second agent makes the fix, and the scanner, not the agent, says when it is gone.

One run handles one service. A finding in twelve services is twelve events and twelve runs: steps run one at a time, and there is no fan-out yet.

| Step | Kind | What it does |
| --- | --- | --- |
| The trigger | a rule | Starts a run for a `scanner.finding` event whose severity is `critical`. Anything lower starts nothing |
| `rank` | deterministic | Notes whether the service faces the internet. Here it only echoes |
| `assess` | ai | An agent says whether the vulnerable function is called, and proposes a plan, inside a $2 budget |
| `approve` | human | An approver accepts or rejects the plan. Whoever started the run cannot |
| `fix` | ai | A second agent makes the fix, inside a $10 budget |
| `open_pull_request` | deterministic | Opens the pull request with the assessment as evidence. Here it only echoes |
| `verify` | external | Asks the scanner every 15 minutes, for up to 7 days, until it reports the finding `resolved` |
| `declined` | deterministic | Where a rejected plan ends |

## What is proven, and what is not

`engine/examples_test.go` runs this file as it is written, with a scripted model and a stand-in scanner:

- a low finding starts no run;
- a critical finding is assessed, approved, fixed and put in a pull request, and the run stays open while the scanner still reports the finding and finishes when it does not;
- a rejected plan ends the run with no fix and no call to the scanner.

It has not been run with a real model or a real scanner. Three things on the website's version of this example are not here, because they are not built or need more setting up:

- **Ranking twelve services against each other.** That needs fan-out.
- **A written reason to override the assessment.** A decision can carry a comment, and nothing requires one.
- **Agents that read and change a repository.** The agents have no tools in this file. A workflow that does this for real, with a checkout, a sandboxed shell and a commit made by code, is described under "An agent that works in a prepared directory" and "Committing what an agent changed" in the main README.

## Run it

You need Postgres (`make up`), a server (`./bin/weir serve`), and a scanner at the `scanner` connection that answers `GET /findings` with a `status`.

```sh
./bin/weir connection scanner https://scanner.example.com
./bin/weir template finding-assess --as ops \
  --system 'You assess whether a vulnerable function in a library is called by a service. You change nothing.' \
  --user 'Service {{service}} uses {{library}}. Say whether the vulnerable function is called, and propose a plan.'
./bin/weir template finding-fix --as ops \
  --system 'You move a service to the lowest version of a library that fixes a vulnerability.' \
  --user 'Service {{service}}, library {{library}}, fixed in {{fixed_in}}. The agreed plan: {{plan}}'
./bin/weir publish examples/vulnerable-library/workflow.yaml --as ops

curl -X POST "$WEIR_URL/v1/events" -H "Authorization: Bearer $WEIR_API_KEY" -H 'Content-Type: application/json' -d '{
  "type": "scanner.finding",
  "id": "scan-2026-11-25-payments",
  "data": {"id": "CVE-2026-0001", "severity": "critical", "service": "payments", "library": "yamlparse", "fixed_in": "2.4.1", "internet_facing": true}
}'

./bin/weir tasks
./bin/weir approve <task-id> --as carol
```
