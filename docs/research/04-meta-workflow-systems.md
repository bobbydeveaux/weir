# Meta's workflow systems

Read on 11 November 2026 from Meta's own engineering posts and one conference page. None of these systems is open source, so this is a source of design ideas, not of code.

Meta has published four relevant systems. They support Weir's core choices (deterministic routing around a single model-calling step, pull-based leases, idempotent transitions) and suggest two additions: a failure policy per step, and circuit breakers in front of third parties. None of the four describes model selection, a model gateway, cost control or an approval inbox, so Weir's gateway and governance have no counterpart here.

## The four systems

| System | Published | What it is |
| --- | --- | --- |
| Thin orchestrator | 19 October 2026 | A deterministic router in front of 14 specialised AI agents |
| Meta Workflow Service (MWFS) | Conference page dated 11 June 2024 | A generic workflow engine that replaced the monolith behind FBLearner Flow |
| Async tier | 31 January 2023 | Meta's platform for asynchronous work, run as a service for other teams |
| FOQS | 22 February 2021 | A multi-tenant priority queue on sharded MySQL |

## Thin orchestrator

[Source](https://engineering.fb.com/2026/10/19/ml-applications/thin-orchestrator-ai-agent-architecture/)

- **The idea.** The orchestrator "performs zero domain work itself. That's the point." It classifies intent, gathers fields, runs reliability checks and delegates. It never generates code or configuration.
- **Routing.** A fixed seven-step protocol per request: read the subagent's instruction file, check for duplicates, gather fields, auto-discover, a reliability gate (active warnings or required reviewers), delegate, handle results. Only the delegation step uses a model, inside the subagent.
- **Templates.** Workflow templates are deterministic state machines, for example "Schema → Validation → Population → Testing". One template per use case reuses the same agents in a different order.
- **Specialists.** Fourteen subagents with an identical structure: knowledge sources, a pre-gathering checklist, a workflow, discovery patterns and troubleshooting. More than 25 knowledge files are loaded at run time.
- **Reported results.** Configuration preparation went from "three to four days by hand" to "under an hour", and about two-thirds of changes land without significant human edits. No methodology is given.
- **Failure.** "If a subagent fails, the orchestrator retries or escalates."
- **Not described.** State storage, leases, model selection, cost control.

## Meta Workflow Service

[Source](https://atscaleconference.com/?p=523576). The page was only partly read (about 40,000 of 52,000 characters); the rest came from a model-generated summary, so some claims below are second-hand.

- **Purpose.** A generic workflow engine built by splitting FBLearner Flow out of a monolith whose database had reached about 1.7 TB.
- **Components.** The core orchestrator, an Event Router (YAML mapping of events to handlers, with filtering), an Action Service (asynchronous execution with automatic retries and completion callbacks) and a Timer Service. Built on SQL and FOQS, with each workflow confined to one shard.
- **Model.** Event-driven: "Events are idempotent and therefore retryable until they succeed." Node states are PENDING, READY, RUNNING, FINISHING and FINISHED.
- **Node types.** A task executor, a sub-workflow executor, a wait executor (timer or external event) and a pass executor for fan-in.
- **Failure policies per node.** Continue processing, block dependents, or cancel dependents, plus a retry-the-workflow operation.
- **Dispatch.** The summary says the team moved from polling to push with callbacks.

## Async tier

[Source](https://engineering.fb.com/2023/01/31/production-engineering/meta-asynchronous-computing/)

- Runs "multi-trillion workloads" a day, re-architected for "five-fold growth over the past two years."
- Three layers: ingestion and storage (FOQS), transport and routing (a scheduler with flow control), and computation.
- Push is a remote call, which "is not a great fit for long-running workloads."
- A controlled-delay service holds failed items with exponential backoff so a clogged stream does not block others.
- Stated lesson: "there is no one-size-fits-all solution when it comes to running async computation at scale."

## FOQS

[Source](https://engineering.fb.com/2021/02/22/production-engineering/foqs-scaling-a-distributed-priority-queue/)

- A priority queue on sharded MySQL with five operations: enqueue, dequeue, ack, nack and list active topics.
- An item carries a priority, a payload of up to 10 KB, a deliver-after time, a lease duration and a time to live.
- An unacknowledged item is redelivered when its lease expires. A nack can defer the item, for backoff, and can update its metadata to keep partial results.
- Pull was chosen on purpose, because it suits consumers of very different kinds.
- Per-shard circuit breakers, global rate limits per namespace.
- Scale in February 2021: "close to one trillion items per day."

## What this means for Weir

**Confirms existing choices**

- Routing, sequencing and gates are deterministic, and only the agent step calls a model. This is the same split as Weir's `decide` and `deterministic` steps around an `ai` step.
- A database-backed queue with leases, acknowledgement and redelivery on expiry works at very large scale.
- State transitions should be idempotent and retryable.
- A wait node for timers and external events matches Weir's parked steps.

**Adopted into the spec**

- A failure policy per step that says what happens to the steps after it: block, cancel or continue.
- Circuit breakers and per-workspace rate limits in front of model providers and third parties.

**Worth considering**

- Keeping slow third-party retries out of the main claim queue.
- A reliability gate before delegating to an agent, such as required reviewers or active warnings.
- Uniform packaging for agents, with knowledge loaded at run time.

**What Meta has that Weir does not plan**

- Sharding by workflow, hot-shard prevention and stuck-workflow detection. These matter at Meta's scale and not at Weir's starting scale.

## Not confirmed

- The MWFS scale figure of more than 1,100 teams and its stated lessons come from the summary, not from text that was read.
- The thin orchestrator's two-thirds figure has no stated methodology.
- An Instagram post by a former Meta engineer prompted this review. It was not seen, so it is not known which of these systems it described.
