# Borrow four engines' vocabulary, build small

The evidence supports designing a custom definition contract that borrows the Serverless Workflow (now Open Workflow Specification) vocabulary, and running it on a custom Postgres engine, not on Temporal or DBOS. The Serverless Workflow DSL 1.x expresses only two of the four required step kinds idiomatically: a deterministic handler (`call`) and an external call resumed by callback (`call` then `listen` with correlation). It has no human task, no model, prompt or budget concept, and its task schema is closed, so AI and human steps can only be encoded as engine-private payloads inside `call ... with` or `metadata`, which removes the portability that would justify adopting the full contract. No runtime implements DSL 1.x in Go on Postgres; the real 1.x runtimes are JVM or .NET, and the only vendor-backed product (Red Hat's SonataFlow line) still documents version 0.8. On the engine question, every existing option either needs an always-on server (Conductor, self-hosted Temporal, Hatchet, Restate, Inngest) or keeps its durability machinery in in-process loops that stop when Cloud Run withdraws CPU (DBOS, River). DBOS Go reached v1.0 in August 2026 and is the most complete library, but its own Cloud Run guide prescribes one always-on instance and its FAQ says the paid Conductor service is required for correct recovery across more than one process. Because the workflow is already versioned data with four fixed step kinds, the engine does not need replay or determinism at all, which is the main reason a small custom engine is credible. The two fallbacks worth keeping in view are DBOS Go on a single always-on instance, and Temporal Cloud with Cloud Run Serverless Workers, which fits the cost constraint on paper but rests on a feature that entered Public Preview in October 2026.

## Three engines interpret data, Temporal replays code

The four systems divide on one question: is the workflow a document an interpreter walks, or a program a worker replays? Conductor, Step Functions and the Serverless Workflow DSL are the former; Temporal is the latter. That choice determines almost everything else, including how versioning works and what must stay running.

### Conductor: a JSON graph plus a separate policy registry

A Conductor workflow is a JSON document identified by `name` and integer `version`, whose `tasks` array holds wiring only (reference name, type, input mapping). Retry, timeout, rate-limit and concurrency policy live in a separately registered **task definition** keyed by task name and shared by every workflow that uses it; only worker-executed `SIMPLE` tasks need one ([workflow definition](https://github.com/conductor-oss/conductor/blob/main/docs/documentation/configuration/workflowdef/index.md), [task definition](https://github.com/conductor-oss/conductor/blob/main/docs/documentation/configuration/taskdef.md)). An abridged version of the documented example:

```json
{
  "name": "mail_a_box",
  "version": 1,
  "tasks": [
    { "name": "shipping_info", "taskReferenceName": "shipping_info_ref",
      "inputParameters": { "account": "${workflow.input.accountNumber}" },
      "type": "SIMPLE" },
    { "name": "shipping_task", "taskReferenceName": "shipping_task_ref",
      "inputParameters": { "name": "${shipping_info_ref.output.name}" },
      "type": "SIMPLE" }
  ],
  "outputParameters": { "trackingNumber": "${shipping_task_ref.output.trackingNumber}" },
  "failureWorkflow": "shipping_issues"
}
```

The open-source server ships roughly ten server-executed system tasks (`HTTP`, `INLINE`, `EVENT`, `WAIT`, `HUMAN`, `KAFKA_PUBLISH`, `JSON_JQ_TRANSFORM`, `NOOP`, `JDBC`) and eleven flow operators (`FORK_JOIN`, `FORK_JOIN_DYNAMIC`, `JOIN`, `EXCLUSIVE_JOIN`, `SWITCH`, `DO_WHILE`, `SUB_WORKFLOW`, `START_WORKFLOW`, `SET_VARIABLE`, `TERMINATE`, `DYNAMIC`) ([system tasks](https://github.com/conductor-oss/conductor/blob/main/docs/documentation/configuration/workflowdef/systemtasks/index.md)). A step-level try/catch operator is still only planned ([roadmap](https://github.com/conductor-oss/conductor/blob/main/ROADMAP.md)).

Pause and resume is a single primitive. The open-source `HUMAN` task takes no parameters and simply "remains in IN_PROGRESS until marked as COMPLETED or FAILED by an external trigger"; completion arrives through the task-update API by task ID or by workflow ID plus reference name, and the posted body becomes the output ([human task](https://github.com/conductor-oss/conductor/blob/main/docs/documentation/configuration/workflowdef/systemtasks/human-task.md)). `WAIT` does the same with an optional duration, any task can be parked with `asyncComplete: true`, and a signal API resolves the first open `WAIT` given only a workflow ID ([wait task](https://github.com/conductor-oss/conductor/blob/main/docs/documentation/configuration/workflowdef/systemtasks/wait-task.md), [signals](https://github.com/conductor-oss/conductor/blob/main/docs/devguide/cookbook/sending-signals.md)). **Assignment, forms, SLAs and escalation exist only in commercial Orkes Conductor**, whose human task carries an ordered `assignments` list, each with an assignee and `slaMinutes`, plus a terminal policy of `LEAVE_OPEN` or `TERMINATE` ([Orkes human task](https://orkes.io/content/reference-docs/operators/human)). Orkes also offers `WAIT_FOR_WEBHOOK`, which resumes a workflow when an inbound payload matches JSONPath-to-value pairs that can reference workflow input, so a third party that does not know the workflow ID can still resume the right instance ([Orkes wait for webhook](https://orkes.io/content/reference-docs/system-tasks/wait-for-webhook)).

Workers are external processes that pull from a per-task-type queue over HTTP or gRPC, by default every 100 ms; the poll is the claim, the lease is `responseTimeoutSeconds` (default 600), and a worker extends it by reporting `IN_PROGRESS` with `callbackAfterSeconds`. Delivery is at-least-once, so "workers should be idempotent" ([architecture](https://github.com/conductor-oss/conductor/blob/main/docs/devguide/architecture/index.md), [task lifecycle](https://github.com/conductor-oss/conductor/blob/main/docs/devguide/architecture/tasklifecycle.md), [workers](https://github.com/conductor-oss/conductor/blob/main/docs/devguide/concepts/workers.md)). The task definition carries a compact retry vocabulary (`FIXED`, `EXPONENTIAL_BACKOFF`, `LINEAR_BACKOFF`, max delay, jitter) and four timeouts: queued too long, worker silent, per attempt, and a total budget across attempts ([task definition](https://github.com/conductor-oss/conductor/blob/main/docs/documentation/configuration/taskdef.md)). Compensation is a convention: the workflow names a `failureWorkflow`, which must read the failed execution to work out which steps completed and undo them in reverse ([saga recipe](https://github.com/conductor-oss/conductor/blob/main/docs/devguide/cookbook/saga-compensation.md)).

Every execution holds a snapshot of the definition taken at start, so "changes to a definition never affect executions that are already running"; upgrading a running instance means terminate and restart. Two weak points stand out: a version number can be overwritten in place, and sub-workflow references float to the latest version unless pinned ([versioning](https://github.com/conductor-oss/conductor/blob/main/docs/devguide/how-tos/Workflows/versioning-workflows.md)). Data mapping uses `${source.input|output.jsonpath}` templates, and JSON Schema validation of workflow and task input and output is in the open-source server, enforced per definition; a failed task-output check fails the task terminally ([schema validation](https://github.com/conductor-oss/conductor/blob/main/docs/devguide/how-tos/schema-validation.md)).

Postgres can serve as database, queue and index, described as "the simplest production stack", with the caveat that it "does not scale as well to handle high volumes" and polls its queues ten times per second per task by default ([deployment](https://github.com/conductor-oss/conductor/blob/main/docs/devguide/running/deploy.md), [PostgreSQL](https://github.com/conductor-oss/conductor/blob/main/docs/documentation/advanced/postgresql.md)). History is the stored workflow and task-attempt records, not an event log. The runtime is a JVM containing continuously running loops: a sweeper without which "long-running workflows will not make progress", and system-task workers polling every 50 ms ([deployment](https://github.com/conductor-oss/conductor/blob/main/docs/devguide/running/deploy.md)). The notes therefore infer that Conductor cannot scale to zero as shipped; at least one always-on instance plus Postgres is the floor.

The project is Apache 2.0. Netflix archived its repository on 13 December 2023 ([Netflix/conductor](https://github.com/Netflix/conductor)); development continues at conductor-oss with Orkes as primary maintainer, and v3.33.0 was marked latest on 26 October with 3.34 release candidates moving the server to Java 25 ([releases](https://github.com/conductor-oss/conductor/releases)). AI support is first-class in the open-source server behind a flag: `LLM_CHAT_COMPLETE`, embeddings and vector search, `LIST_MCP_TOOLS` and `CALL_MCP_TOOL`, and A2A agent tasks, all as ordinary persisted steps with the same input mapping and retry mechanism ([AI tasks](https://github.com/conductor-oss/conductor/blob/main/docs/documentation/configuration/workflowdef/systemtasks/ai-tasks.md)). Prompt text sits inline in the workflow definition; a prompt management studio is an Orkes feature ([OSS vs Orkes](https://www.orkes.io/platform/conductor-oss-vs-orkes)).

### Step Functions: one Task state, selected by a resource URI

An Amazon States Language definition is one JSON document: a `StartAt` pointer and a flat `States` map joined by `Next` strings. There are eight state types (`Task`, `Choice`, `Parallel`, `Map`, `Wait`, `Pass`, `Succeed`, `Fail`); only `Task` does external work, and what it does is chosen entirely by its `Resource` URI ([ASL specification](https://states-language.net/spec.html)).

```json
{
  "StartAt": "Hello World",
  "States": {
    "Hello World": {
      "Type": "Task",
      "Resource": "arn:aws:lambda:us-east-1:123456789012:function:HelloWorld",
      "Retry": [ { "ErrorEquals": ["States.Timeout"], "IntervalSeconds": 3,
                   "MaxAttempts": 3, "BackoffRate": 2, "JitterStrategy": "FULL" } ],
      "End": true
    }
  }
}
```

The pause mechanism is the **task token**. A `Task` whose resource ends in `.waitForTaskToken` hands out an opaque token and parks until someone returns it with `SendTaskSuccess` or `SendTaskFailure`; AWS names human approval and third-party integration as the purpose. A new token is generated for each attempt, and a waiting task with no timeout waits until the one-year execution limit, so AWS tells authors to set `HeartbeatSeconds` ([integration patterns](https://docs.aws.amazon.com/step-functions/latest/dg/connect-to-resource.html)). Pull-based workers ("Activities") use the same completion API: the worker polls `GetActivityTask`, receives input and a token, heartbeats, and reports the result ([Activities](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-activities.html)). There is no human-task inbox, assignment or escalation; a human step is "send the token somewhere and have something call back". Third-party HTTPS calls have their own resource (`http:invoke`) with credentials held outside the definition in a named connection, a hard 60-second limit, and HTTP status codes surfaced as named errors such as `States.Http.StatusCode.429` ([HTTP Task](https://docs.aws.amazon.com/step-functions/latest/dg/call-https-apis.html)). Engine-side polling of a long job (`.sync`) exists only for AWS services, so polling a third party is a hand-built `Wait` and `Choice` loop, which the notes state as standard practice, not a quoted AWS pattern.

Errors are strings matched by name. `Retry` is an ordered list of per-error backoff policies; `Catch` is an ordered list of per-error transitions that applies once retries are exhausted. Two reserved errors (`States.Runtime`, `States.DataLimitExceeded`) are not matched by the `States.ALL` wildcard ([error handling](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-error-handling.html)). There is no compensation construct. Versioning is the cleanest of the four: every save is an immutable revision, publishing mints a monotonically numbered version that is never reused, aliases point at up to two versions with traffic weights, and "running executions will continue to use the previous definition" ([versions](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-state-machine-version.html), [aliases](https://docs.aws.amazon.com/step-functions/latest/dg/concepts-state-machine-alias.html), [UpdateStateMachine](https://docs.aws.amazon.com/step-functions/latest/apireference/API_UpdateStateMachine.html)). Redrive restarts a failed execution from the failed state within 14 days, always on the original definition ([redrive](https://docs.aws.amazon.com/step-functions/latest/dg/redrive-executions.html)).

Standard workflows keep an append-only, sequentially numbered event history (scheduled, started, succeeded, failed, timed out, per state) capped at **25,000 events** and retained 90 days after close ([HistoryEvent](https://docs.aws.amazon.com/step-functions/latest/apireference/API_HistoryEvent.html), [quotas](https://docs.aws.amazon.com/step-functions/latest/dg/limits-overview.html)). Data flow originally threaded one JSON document through five JSONPath filters per state; since November 2024 a JSONata mode reduces that to `Arguments` and `Output` and adds workflow variables through `Assign`, with all expressions reading values as of state entry ([JSONata](https://docs.aws.amazon.com/step-functions/latest/dg/transforming-data.html), [variables](https://docs.aws.amazon.com/step-functions/latest/dg/workflow-variables.html)). Payloads are capped at 256 KiB. The notes found no field for declaring or validating a state's input or output schema; AWS says of its own model integration that it "doesn't validate the input you provide in `Body`" ([Bedrock integration](https://docs.aws.amazon.com/step-functions/latest/dg/connect-bedrock.html)).

Step Functions is a proprietary managed service billed per state transition for Standard workflows, so the user runs nothing ([workflow types](https://docs.aws.amazon.com/step-functions/latest/dg/choosing-workflow-type.html)). As a portable contract ASL is weak: AWS's own local runner "does not provide feature parity and is unsupported" ([Step Functions Local](https://docs.aws.amazon.com/step-functions/latest/dg/sfn-local.html)), and the independent interpreters the notes found are small, partial or stale. AI remains "a Task with a Bedrock resource". The June 2026 agent step (`bedrockagentcore:invokeHarness`) takes a model, system prompt, messages, tools and `MaxIterations`, and returns only the final assistant text plus token usage; it is capped at 15 minutes, supports no callback pattern, and "stopping an execution or the Task state does not stop the harness" ([AgentCore integration](https://docs.aws.amazon.com/step-functions/latest/dg/connect-bedrockagentcore.html)). ASL itself has no prompt, tool or schema construct.

### Serverless Workflow DSL: twelve verbs in a closed schema

The project was renamed in mid-2026: `serverlessworkflow/specification` now redirects to `open-workflow-specification/specification`. The latest tag is v1.0.3, `main` declares 1.0.4, and a v1.1.0 milestone was 97% complete with a due date of 13 November 2026 ([releases](https://github.com/open-workflow-specification/specification/releases), [milestones](https://github.com/open-workflow-specification/specification/milestones)). A 1.x workflow is a YAML or JSON document with a mandatory `document` header and a `do` list of single-key task maps. The example below is composed from two snippets in the spec, not copied verbatim, and has not been validated against the schema:

```yaml
document:
  dsl: '1.0.3'
  namespace: default
  name: order-check
  version: '0.1.0'
do:
  - getPet:
      call: http
      with:
        method: get
        endpoint: https://petstore.swagger.io/v2/pet/{petId}
      output:
        as:
          pet: ${ .content }
  - route:
      switch:
        - sold:
            when: .pet.status == "sold"
            then: end
        - default:
            then: continue
```

There are exactly twelve task kinds runtimes must implement: `call`, `do`, `emit`, `for`, `fork`, `listen`, `raise`, `run`, `set`, `switch`, `try`, `wait`. Every task also accepts `if`, `input`, `output`, `export`, `timeout`, `then` and `metadata` ([task reference](https://github.com/open-workflow-specification/specification/blob/main/dsl-reference.md#task)). Built-in `call` targets are `http`, `grpc`, `openapi`, `asyncapi`, `a2a` and `mcp`; any other string is a custom function. The task definition in the schema is `unevaluatedProperties: false` over a `oneOf` of the twelve, so **an unknown verb fails validation** ([schema](https://github.com/open-workflow-specification/specification/blob/main/schema/workflow.yaml)).

Pause and resume is `listen` on CloudEvents, with attribute filters and a `correlate` block that extracts a value from the event and compares it with an expected value; the generic task `timeout` raises a catchable error of type `.../errors/timeout` ([event filter](https://github.com/open-workflow-specification/specification/blob/main/dsl-reference.md#event-filter), [timeouts](https://github.com/open-workflow-specification/specification/blob/main/dsl.md#timeouts)). How an event reaches a runtime is left to implementations. The DSL describes no worker model at all, and says nothing normative about durability, idempotency or what is persisted between tasks; the maintainers' own issue records that "execution behavior is described across prose sections, with no single normative semantics" ([issue #1212](https://github.com/open-workflow-specification/specification/issues/1212)).

Errors are RFC 7807 problem details with eight standard type URIs. Retry (delay, constant, linear or exponential backoff, jitter range, attempt and duration limits) can be attached only through a `try`/`catch` wrapper; a request to allow `catch` on any task has been open since March 2025 ([retry](https://github.com/open-workflow-specification/specification/blob/main/dsl-reference.md#retry), [issue #1091](https://github.com/open-workflow-specification/specification/issues/1091)). Compensation existed in 0.x and is absent from 1.x. `document.version` is a required semantic version and `run.workflow` can pin a namespace, name and version, but the spec says nothing normative about running instances when a definition changes, and has no published compatibility policy for the DSL itself ([issue #1211](https://github.com/open-workflow-specification/specification/issues/1211)).

Data flow is the DSL's strongest part: each task validates input (`input.schema`), transforms it (`input.from`), runs, transforms output (`output.as`), validates it (`output.schema`) and optionally rewrites shared context (`export.as`), with each failed check raising a typed `validation` error. jq is the mandatory expression language and JSON Schema the only schema format ([data flow](https://github.com/open-workflow-specification/specification/blob/main/dsl.md#data-flow), [expressions](https://github.com/open-workflow-specification/specification/blob/main/dsl.md#runtime-expressions)). Composition is `run.workflow` for sub-workflows, `fork` for parallel branches (with a `compete` race mode), `switch` for branching and `for` for iteration; there is no while loop, and a 56-comment request for one remains open ([issue #1096](https://github.com/open-workflow-specification/specification/issues/1096)).

The spec is Apache-licensed tooling around a **CNCF Sandbox** project accepted on 14 July 2020 and still at Sandbox, with two listed maintainers ([README](https://github.com/open-workflow-specification/specification/blob/main/README.md), [maintainers](https://github.com/open-workflow-specification/specification/blob/main/MAINTAINERS.md)). The AI-adjacent additions are `call: a2a` and `call: mcp`, both in v1.0.2. There is no agent, LLM, prompt or inference task in any released version; an "agent characteristics" proposal is open against v1.2.0, where a maintainer argued for a first-class `infer` task instead of an extension ([issue #1184](https://github.com/open-workflow-specification/specification/issues/1184)).

### Temporal: deterministic code over an event history

A Temporal workflow is ordinary code that must be deterministic, because the server stores an append-only event history and workers rebuild state by replaying it. Side effects go in activities, which retry by default with unlimited attempts, a 1-second initial interval and a 2.0 backoff coefficient; workflows do not retry by default ([retry policies](https://docs.temporal.io/encyclopedia/retry-policies)). The following Go sketch is illustrative and was not compiled by the researchers; it shows an activity followed by a human approval raced against a durable timer.

```go
func ApprovalWorkflow(ctx workflow.Context, ticketID string) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 5},
	})
	var draft string
	if err := workflow.ExecuteActivity(ctx, DraftReply, ticketID).Get(ctx, &draft); err != nil {
		return "", err
	}
	var decision Approval
	timedOut := false
	sel := workflow.NewSelector(ctx)
	sel.AddReceive(workflow.GetSignalChannel(ctx, "approval"),
		func(c workflow.ReceiveChannel, _ bool) { c.Receive(ctx, &decision) })
	sel.AddFuture(workflow.NewTimer(ctx, 7*24*time.Hour),
		func(workflow.Future) { timedOut = true })
	sel.Select(ctx)
	if timedOut || !decision.Approved {
		return "rejected", nil
	}
	return draft, nil
}
```

There are no built-in step types: a step is whatever an activity does. Human input arrives as a **signal** (asynchronous, cannot return a result) or an **update** (synchronous, validated before it is accepted into history, and able to return an outcome to the sender) ([message passing](https://docs.temporal.io/encyclopedia/workflow-message-passing)). Workers are the user's processes holding outbound long-polls on task queues; the server never calls in, and it does not detect a crashed worker except through the Start-To-Close timeout, which Temporal says always to set ([detecting activity failures](https://docs.temporal.io/encyclopedia/detecting-activity-failures)). Activities have four timeouts (schedule-to-start, start-to-close per attempt, schedule-to-close overall, heartbeat). Compensation is user code.

Versioning is where workflows-as-code costs most. Changing workflow code under a running execution breaks replay unless handled by patching or by Worker Versioning, which reached general availability on 30 March 2026 and pins a workflow to the worker build it started on; Temporal itself concedes that "patches accumulate into real code complexity" ([Worker Versioning GA](https://temporal.io/blog/ga-worker-versioning-public-preview-upgrade-on-continue-as-new)). History is a complete per-run audit trail but is capped at 51,200 events or 50 MB with a 2 MB payload limit, and self-hosted Temporal has no RBAC or audit logging out of the box ([defaults](https://docs.temporal.io/self-hosted-guide/defaults), [production checklist](https://docs.temporal.io/self-hosted-guide/production-checklist)). There is no schema validation or mapping layer; typing comes from the host language.

The server is MIT licensed ([temporalio/temporal](https://github.com/temporalio/temporal)). Self-hosted, it can run on Postgres alone, since PostgreSQL 12+ is a supported visibility store ([visibility](https://docs.temporal.io/self-hosted-guide/visibility)), but the notes found nothing suggesting the server can scale to zero. Workers can: Serverless Workers on Cloud Run, in Public Preview, let Temporal resize a Cloud Run Worker Pool, including "to zero" when there is no work, and require Worker Versioning ([Serverless Workers on Cloud Run](https://docs.temporal.io/serverless-workers/cloud-run)). Since 14 September 2026 Temporal Cloud has a Developer plan with no monthly minimum at $50 per million actions plus storage and 10% support ([pricing announcement](https://temporal.io/blog/paygo-developer-support)). AI support is a major 2026 focus, with the consistent model that the agent loop is workflow code and every model, tool and MCP call is an activity, but the first-party integrations are almost all Python; the researchers found nothing Go-specific. The Temporal Agent Harness, announced 20 August 2026, describes itself as "earlier than public preview" ([Agent Harness](https://temporal.io/blog/temporal-agent-harness-durable-agent-infrastructure)).

## The four systems side by side

The table condenses the sections above; sources are cited there. Cells marked (inferred) are the researchers' reading, not a quoted statement.

| Dimension | Conductor (OSS / Orkes) | Step Functions (ASL) | Serverless Workflow DSL 1.x | Temporal |
|---|---|---|---|---|
| Definition | JSON data; `tasks` array plus separate task definitions | JSON data; `StartAt` plus flat `States` map | YAML/JSON data; `document` header plus `do` list | Code (Go, Java, Python, TypeScript, .NET, Ruby) |
| Step types | About 10 system tasks, 11 operators, AI task family | 8 state types; `Task` kind chosen by `Resource` URI | 12 task verbs; 6 built-in `call` targets | None; activities are user code |
| Human task | OSS: bare parked task. Orkes: assignment, forms, SLA escalation chain | None; task token sent to a human | None; `emit` then `listen` by convention | None; signal or update raced against a timer |
| Third-party callback | Task update by ID or reference; Orkes payload-matching webhook | `.waitForTaskToken`, token per attempt | `listen` with CloudEvent `correlate` | Signal, update, or async activity completion |
| Worker model | External pull workers, 100 ms poll, poll is the claim, response-timeout lease | Managed integrations; Activities pull with token and heartbeat | Not specified | User workers long-poll task queues |
| Retry and timeouts | Policy on task definition; 4 timeouts; backoff with jitter | `Retry` and `Catch` lists on the state; named errors | Retry only inside `try`/`catch`; RFC 7807 errors | Activity retry policy; 4 timeouts; unlimited attempts by default |
| Compensation | `failureWorkflow` convention | `Catch` routing by hand | None in 1.x | User code |
| Definition versioning | `name` + integer; versions mutable; children float to latest | Immutable numbered versions, weighted aliases | Semantic version required; nothing normative beyond that | Worker build versions; patching |
| Running instances | Snapshot at start; upgrade by terminate and restart | Pinned to start definition; redrive on same definition | Implementation-defined | Pinned or auto-upgrade; replay must match |
| Persistence and audit | Postgres can be database, queue and index; state records, not an event log | Managed; append-only history, 25,000 events, 90 days | Not specified; lifecycle CloudEvents | Event history, 51,200 events; Postgres supported |
| I/O mapping and schema | `${ref.output.path}` JSONPath; JSON Schema enforced per definition | JSONPath filters or JSONata plus variables; no schema validation found | jq; JSON Schema on input, output and export | Host-language types |
| Sub-workflow, parallel, branch | `SUB_WORKFLOW`, `FORK_JOIN` (static and dynamic), `SWITCH`, `DO_WHILE` | Task calling `startExecution`, `Parallel`, `Map`, `Choice` | `run.workflow`, `fork`, `switch`, `for` | Child workflows, selectors, native control flow |
| Footprint and scale to zero | Always-on JVM plus Postgres; no (inferred) | Fully managed; nothing to run | Depends on runtime | Self-hosted server always-on (inferred); Cloud plus serverless workers can idle at zero (preview) |
| Licence and status | Apache 2.0; Netflix archived Dec 2023; Orkes-led, very active | Proprietary service; public spec | CNCF Sandbox since 2020; renamed 2026; two maintainers | MIT server; commercial Cloud; very active |
| AI support | `LLM_*`, MCP and A2A tasks in OSS behind a flag | Bedrock `invokeModel`; AgentCore harness step (2026) | `call: a2a`, `call: mcp`; no LLM task | Agent integrations, mostly Python; LLM call is an activity |

## The Serverless Workflow DSL models two of four step kinds

The question was whether the DSL can express AI, deterministic, human and external steps natively or through its extension mechanisms. It has four extension points: custom functions invoked through `call` (declared in `use.functions`, referenced by URL, or resolved from a catalog as `name:version@catalog`), `use.extensions` (before and after wrappers around existing task types), free-form `metadata` on the document and every task, and alternative expression languages. None of them can add a task verb ([extensions](https://github.com/open-workflow-specification/specification/blob/main/dsl-reference.md#extension), [schema](https://github.com/open-workflow-specification/specification/blob/main/schema/workflow.yaml)). The findings on each kind below are the researchers' analysis of the documented syntax; the YAML sketch is theirs, not the spec's.

**Deterministic step (registered code handler): native.** This is the DSL's natural case: `call: compute-tax:1.4.0@default`. The spec lets a runtime define a default catalog and says how its resources "are stored and resolved is entirely up to the runtime" ([default catalog](https://github.com/open-workflow-specification/specification/blob/main/dsl.md#default-catalog)), so a Go handler registry qualifies. One wrinkle is that a catalogued function is formally a task definition, so a name that maps to compiled Go code is an implementation liberty.

**External step (call a third party, resume on callback or poll): callback is native, polling is hand-built.** `call: http` followed by `listen` with `correlate` and a task `timeout` covers the callback half well. Polling has no declarative construct: it must be written as a call, a `wait` and a backwards `then` jump guarded by `if` or `switch`, or as a `try` with a retry policy around a call that raises while the job is unfinished. Flow directives "may only redirect to tasks declared within their own scope" ([flow directive](https://github.com/open-workflow-specification/specification/blob/main/dsl-reference.md#flow-directive)), and a contributor noted that such loops reuse the same JSON pointer each lap, so lifecycle monitors cannot tell iterations apart ([issue #1096](https://github.com/open-workflow-specification/specification/issues/1096)).

**AI step (pinned prompt template version, JSON-Schema-checked output, cost budget): only as an opaque custom function.** Of the three required properties, the DSL supplies one natively. `output.schema` validates against JSON Schema and faults with a standard `validation` error that a surrounding `try` can catch and retry. A catalog reference gives a versioned name, but the spec does not state that a catalog version is immutable. There is no model, prompt or budget concept and no budget error type, so those ride as engine-private data:

```yaml
- classifyTicket:
    call: prompt.classify-ticket:3.2.0@default
    with:
      model: some-model-id
      variables: { ticket: "${ .ticket }" }
      budget: { maxCostUsd: 0.05 }
    output:
      schema:
        format: json
        document:
          type: object
          required: [ category, confidence ]
```

Two details matter. Validation runs after `output.as`, so the schema checks the transformed output, not the raw model response. And `call: mcp` with `prompts/get` retrieves a prompt from a server; it does not execute one against a model, so neither `mcp` nor `a2a` substitutes for an inference step. The open agent proposal discusses model, instruction, tools and memory; nothing in it mentions pinned prompt versions or cost budgets ([issue #1184](https://github.com/open-workflow-specification/specification/issues/1184)).

**Human step (role-assigned approval with timeout and escalation): not native.** The DSL has no assignee, role, form, deadline or escalation concept, and the project's own comparison lists "human-centric workflows" under BPMN, not itself ([comparison](https://github.com/open-workflow-specification/specification/blob/main/comparison.md)). The idiom is `emit` a request event, `listen` for a correlated decision with a `timeout`, wrap both in `try`, and in the `catch` emit an escalation event and listen again. The researchers' composition runs to roughly 40 lines for what a purpose-built contract writes as four fields, and the role and escalation target are opaque event data. Authorisation, inbox, reassignment and the audit of who decided all fall outside the DSL. The terser alternative, `call: approval` with role and timeout in `with`, is schema-valid but entirely private to the engine.

### No runtime implements DSL 1.x in Go on Postgres

The specification's README lists five runtimes: Apache KIE SonataFlow, the Java SDK reference implementation, Quarkus Flow, Lemline and Synapse ([README](https://github.com/open-workflow-specification/specification/blob/main/README.md)). Their actual state is uneven.

| Runtime | Language | DSL version | Persistence | Licence | Notes |
|---|---|---|---|---|---|
| Synapse | C# | 1.x | Not verified | Apache 2.0 | 314 stars; API server, operator, per-instance runner, correlator ([repo](https://github.com/serverlessworkflow/synapse)) |
| Java SDK `impl` | Java | 1.0.0 | Pluggable | Apache 2.0 | Described as fully compliant; 7.35.3 in October 2026 ([repo](https://github.com/open-workflow-specification/sdk-java)) |
| Quarkus Flow | Java | 1.0.0 | JPA including PostgreSQL | Not verified | Supports only `run.workflow` of the `run` variants ([repo](https://github.com/quarkiverse/quarkus-flow)) |
| Lemline | Kotlin | 1.0 | Broker messages plus Postgres or MySQL | BSL 1.1 per its docs | v0.6.3, 43 stars ([repo](https://github.com/lemline/lemline)) |
| Zigflow | Go | 1.0 subset with extensions | Temporal | Apache 2.0 | Compiles YAML to a Temporal workflow; changes the `document` block and adds `call: activity` ([repo](https://github.com/zigflow/zigflow)) |
| EventMesh Workflow | Go | 1.0.3 and 0.8 | MySQL | Apache 2.0 | Wrote its own parser instead of the official Go SDK ([repo](https://github.com/apache/eventmesh-workflow)) |
| SonataFlow / OpenShift Serverless Logic | Java | **0.8 only, partial** | Not verified | Apache 2.0 upstream | The only vendor-backed product ([spec support](https://kie.apache.org/docs/10.2.x/sonataflow/serverlessworkflow/1.43.0.Final/core/cncf-serverless-workflow-specification-support.html)) |

For a Go builder, the usable asset is the Go SDK: v4.0.0 (July 2026, import path changed by the rename) parses, builds and schema-validates DSL 1.0.0 documents. Its bundled runner is explicitly partial, with `call`, `emit`, `listen`, `run`, `try`, `wait`, retry and timeout all unimplemented ([sdk-go README](https://github.com/open-workflow-specification/sdk-go/blob/main/README.md)). The Python and PHP SDKs still target 0.8. Conformance is self-declared: runtimes "merely should pass the CTK" and "there are no conformance classes, no core vs. optional split" ([issue #1208](https://github.com/open-workflow-specification/specification/issues/1208)). The researchers found no evidence of one definition running unchanged on two independent runtimes, and no named production adopter of DSL 1.x.

## Recommendation 1: a custom contract in the DSL's vocabulary

Design a custom contract, borrow the Serverless Workflow vocabulary and shapes, and keep a one-way export path open. Do not adopt the DSL, in full or as a declared subset.

The decisive point is that the two step kinds that differentiate this engine are the two the DSL does not model. Adopting it would buy a standard envelope around engine-private payloads, and such documents would not run meaningfully anywhere else. The cost side is concrete. The mandatory surface is heavy for a thin Go service: twelve task types, protocols including gRPC and AsyncAPI, containers, shell and script execution, a jq evaluator, and CloudEvents ingestion; a realistic implementation would be a subset, and the spec has no way to claim subset conformance. The closed schema forbids new top-level keys, so `budget` or `assignee` cannot be first-class and schema-valid at once. Retry demands a `try` wrapper around every retryable step. And the DSL is silent on exactly what a claim/lease engine must get right: durability, idempotency, at-least-once execution, and instance versioning.

Stability counts against it too. The contract changed identity twice in about eighteen months: a "total refactor" from the 0.8 state machine to 1.0 tasks in January 2025 ([v1.0.0 notes](https://github.com/open-workflow-specification/specification/releases/tag/v1.0.0)), then a rename that changed the organisation, domain, schema `$id`, Go module path and event namespaces ([issue #1127](https://github.com/open-workflow-specification/specification/issues/1127)). New call types arrived in patch releases, and one field type was changed and reverted within months. In November 2026 the maintainers opened a seven-part programme acknowledging missing conformance classes, versioning policy, normative execution semantics and security considerations ([issue #1214](https://github.com/open-workflow-specification/specification/issues/1214)).

The strongest case for staying closer to the DSL is tooling and familiarity: a Go parser and validator, an embeddable visual editor, a published JSON Schema, and the likelihood that language models can draft documents in a known format. That case is weakened by the volume of 0.8-era material, which leads authors of either kind to produce documents that are invalid in 1.x. Borrowing vocabulary captures most of the familiarity benefit without the obligations. If a first-class `infer` task is standardised in 1.2 or later, a custom contract shaped like the DSL can export to it.

Concretely, the contract should take from the DSL the `document` header (`namespace`, `name`, `version`, plus a contract-version field), an ordered step list, per-step `input` and `output` with JSON Schema, `timeout.after`, the retry shape, RFC 7807 errors with engine-defined type URIs, CloudEvents-style correlation for waits, and the status-phase names. It should add a `kind` discriminator with four values and a kind-specific block, put retry policy directly on the step, and declare compensation next to the step it undoes. On expressions, the report's judgement is to avoid making jq part of the public contract: Conductor's reference syntax (a source name, `input` or `output`, and a path) is small enough to validate against step output schemas at publish time, which removes its documented hazard of a "silently-null `${...}` expression" ([schema validation](https://github.com/conductor-oss/conductor/blob/main/docs/devguide/how-tos/schema-validation.md)).

## Recommendation 2: a custom Postgres engine, with two named fallbacks

Build the engine. Do not build on Temporal or DBOS for the first version.

### Why not Temporal

Self-hosted Temporal is four always-on roles plus a database, with sequential minor-version upgrades, shard count fixed at build time and no RBAC or audit logging ([production checklist](https://docs.temporal.io/self-hosted-guide/production-checklist)). That is the cost profile the owner already abandoned. Temporal Cloud changes the arithmetic: with no base fee, 100,000 actions cost $5 plus $0.50 support before storage ([pricing announcement](https://temporal.io/blog/paygo-developer-support)), and Cloud Run Worker Pools can idle at zero. That combination fits the constraint on paper, but it depends on a Public Preview feature, requires Worker Versioning with a separate pool per version, and replaces "Postgres only" with a managed dependency.

It also saves less than it appears to. The supported way to run a declarative definition on Temporal is one generic interpreter workflow that receives the definition as input; the official Go sample covers only sequence, parallel and activity, with no conditionals, loops, signals, timers or per-step retry options ([samples-go dsl](https://github.com/temporalio/samples-go/blob/main/dsl/workflow.go)). Temporal would supply durable timers, retries, dispatch and crash recovery. The contract, validation, definition storage and versioning, approval inbox and the product's own run tables are the same work either way, and the interpreter's own code changes then need replay-safe versioning.

### Why not DBOS, and the state of DBOS Go

DBOS Transact is an MIT-licensed in-process library that checkpoints workflows and steps to a `dbos` schema in Postgres, with "no separate orchestration server" ([architecture](https://docs.dbos.dev/architecture)). **The Go SDK was announced as v1.0 on 27 August 2026**, claiming "feature parity across the core DBOS programming model" and a stable public API ([August 2026 update](https://dbos.dev/blog/new-in-dbos-august-2026)); the upgrade guide already references v1.8.0. It covers workflows, steps, durable sleep, send and receive, events, streams, database-backed queues with concurrency and rate limits, schedules, fork, resume and rewind. v1.6.0 added fencing tokens so that an execution that lost ownership cannot checkpoint over the current owner; v1.7.0 moved the Conductor client into a separate enterprise module; v1.8.0 made automatic version upgrade an enterprise feature ([Go upgrading guide](https://docs.dbos.dev/golang/upgrading)). The repository had 870 stars at the time of research ([repo](https://github.com/dbos-inc/dbos-transact-golang)).

Three facts rule it out as the substrate here. First, "Conductor is required for correct workflow recovery in applications that use more than one process" ([FAQ](https://docs.dbos.dev/faq)), and Conductor starts at $99 per month with self-hosting reserved for Enterprise ([pricing](https://www.dbos.dev/dbos-pricing)). Second, DBOS's own Cloud Run guide prescribes `--no-cpu-throttling` and `--min-instances=1`, or a worker pool with at least one instance, so that its background services keep running ([Cloud Run hosting](https://docs.dbos.dev/production/hosting-with-cloud-run)). Third, its versioning model hashes workflow source code and recovers only matching versions, which guards the interpreter and not the definitions. The researchers add, as inference, that a multi-day human wait parks a goroutine in a live process and that DBOS state would be double-booked against the product's own run and step tables.

### Why the other Postgres-backed options do not fit

| Option | Model | Licence | What must stay running | Fit |
|---|---|---|---|---|
| River / River Pro | Go job queue; Pro adds DAG workflows with signals, timers and CEL waits | MPL-2.0 / commercial | A worker process | Workflows are in the paid, closed tier ([River](https://github.com/riverqueue/river), [Pro workflows](https://riverqueue.com/docs/pro/workflows)) |
| Hatchet | Engine plus workers on Postgres | MIT | The engine | Always-on server ([repo](https://github.com/hatchet-dev/hatchet)) |
| go-workflows | Embedded Temporal-like library | MIT | A worker process | Small project, replay model ([repo](https://github.com/cschleiden/go-workflows)) |
| Restate | Server pushes to handlers | BSL runtime (secondary source) | Restate server on RocksDB | Not Postgres |
| Inngest | Server pushes HTTP to functions | Delayed open source ([self-hosting](https://www.inngest.com/docs/self-hosting)) | Inngest server | Always-on server |
| pgflow | SQL core with edge workers | Apache 2.0 | Supabase stack | No Go SDK |
| Absurd | Single SQL file of stored functions, pull workers | Apache 2.0 | A worker process | Go SDK marked experimental; useful as a blueprint ([repo](https://github.com/earendil-works/absurd)) |

### What the custom engine must get right

The argument for building rests on one observation: a data-defined engine with fixed step kinds can persist "current step plus step outputs" transactionally and never replay code, so the determinism, patching and history-size machinery that Temporal and DBOS exist to manage is not needed. The wake-up plumbing has to be built whichever library is chosen, because of how Cloud Run works. Under default billing "CPU is allocated when instances are processing requests", and "a service that is not processing requests cannot scale from zero"; Google's guidance is minimum instances or a "wake-up request" in the design ([container contract](https://docs.cloud.google.com/run/docs/container-contract), [autoscaling](https://docs.cloud.google.com/run/docs/about-instance-autoscaling)). A poll loop, an in-memory timer, a lease heartbeat and a LISTEN connection all stop when the instance does.

The pattern the researchers derive from that guidance is push with a sweep. Each transition that makes a step runnable writes an outbox row in the same transaction and rings a doorbell (a Cloud Tasks HTTP task or Pub/Sub push) so the step executes inside a request. A Cloud Scheduler tick claims due timers, expired leases and any runnable rows whose doorbell was lost. Steps that can outlast a request run as a Cloud Run job. A human approval consumes nothing while waiting: the row sits in a waiting state and the approval request performs the transition. This is inference; no source documents an engine built this way on Cloud Run with Cloud SQL.

The correctness rules come from where home-grown Postgres queues fail. Claim in a short transaction that sets a lease and increments an attempt counter, run the step outside any transaction, and complete with a write conditioned on that attempt so a stale worker is rejected; DBOS adding fencing tokens in v1.6.0 is the supporting evidence that mature libraries need this. Treat AI and third-party calls as at-least-once with idempotency keys. Keep timers as rows. Keep hot tables small with partial indexes and archiving, since practitioners report that the claim query "is fine until the worker count climbs", after which vacuum falls behind ([Hacker News discussion](https://news.ycombinator.com/item?id=48313530)). At the volume implied by human approvals and AI steps, bloat is manageable; fencing, idempotency and the outbox are where such an engine fails first.

The counter-position deserves its due. A DBOS engineer in the same discussion says teams that built their own "eventually moved on to buying vs building", an interested party but a fair warning. Two triggers should reopen this decision: if one always-on small instance becomes acceptable, DBOS Go in a single process avoids the Conductor requirement and supplies queues, sleep and messaging immediately; and if Temporal's Cloud Run Serverless Workers reach general availability and prove out, Temporal Cloud becomes a legitimate substrate for the same custom contract.

## Design ideas worth taking from each system

| From | Idea | Reason |
|---|---|---|
| Conductor | One `type` discriminator on the step, with a kind-specific config block | AI steps are ordinary steps with the same mapping, persistence and retry; no separate subsystem needed |
| Conductor | Each retry is a new attempt record linked to the previous one, with worker ID and timestamps | Gives a per-attempt audit trail without extra design |
| Conductor | Four distinct clocks: queued too long, worker silent (lease), per attempt, total budget across attempts | Each detects a different failure; the total budget bounds retries in time, not only count |
| Conductor | Two resume addresses: by run ID plus step reference, and by correlation match on the inbound payload | A third party does not know the run ID; payload matching is what callbacks need |
| Conductor | JSON Schema on step input and output, failing before the worker runs or after it returns | The natural guard for AI structured output |
| Conductor (avoid) | Mutable versions, unversioned task definitions, children floating to latest | Each lets behaviour change without the definition changing; publish immutable versions and resolve references at publish time |
| Orkes | Human escalation as an ordered list of assignees, each with an SLA, then a terminal policy | A compact data model for role assignment, timeout and escalation |
| Step Functions | An unguessable token per attempt, rotated on retry | Late completions from a dead attempt are rejected, not applied; one token table serves workers, humans and callbacks |
| Step Functions | Immutable revisions, monotonically numbered versions never reused, named aliases with weights | Runs pin a version; callers use a stable alias; canary rollout is a random draw at start |
| Step Functions | Append-only numbered event log with scheduled, started, succeeded, failed, timed out | The log, not the current-state row, is the audit record; scheduled versus started yields queue latency |
| Step Functions | Named connection objects holding credentials, referenced from the definition | Keeps secrets out of versioned workflow data |
| Step Functions | Reserved engine errors that a catch-all does not match | Engine and definition bugs should not be swallowed by a wildcard handler |
| Step Functions | Variables read as of step entry, computed, then assigned; isolated scopes per parallel branch | Deterministic, race-free updates in a single row |
| Step Functions | Redrive from the failed step keeping completed results; idempotent start by run name | Cheap operator repair and duplicate-start protection |
| Step Functions | Fan-out controls: max concurrency, tolerated failure threshold, batching | Protects downstream limits; concurrency doubles as a model rate-limit guard |
| Serverless Workflow | `document` header, ordered step list, `input`/`output`/`export` with schemas | A small, regular, well-specified shape that maps to a definitions table |
| Serverless Workflow | RFC 7807 errors with type URIs, including engine-defined types | A typed error model that extends to a budget-exceeded error |
| Serverless Workflow | Retry shape: delay, backoff kind, jitter range, attempt and duration limits | Complete and copyable even without the rest of the DSL |
| Serverless Workflow | `listen` with `correlate` (extract from event, compare with expected) | A waiting step becomes a row with correlation keys; nothing runs while waiting |
| Serverless Workflow (avoid) | Retry only via `try`, no loop construct, no compensation | Each forces verbose or hand-written patterns for common needs |
| Temporal | Persist a non-deterministic step's output once and never recompute on resume | What makes resumption safe with a model in the loop |
| Temporal | Signal versus validated update for inbound input | An approval endpoint should reject input for a step that is no longer waiting and return the outcome |
| Temporal | Separate per-attempt and overall timeouts, plus a non-retryable error list | Four numbers and a list cover most real cases |
| Temporal (avoid) | Unlimited retry attempts by default | Each AI retry costs money; bound attempts on AI steps |
| Temporal | Payload size limits with external blob storage and references | Documents and transcripts overflow step records quickly |
| Temporal | An explicit upgrade at a safe boundary for long-lived runs | Pinning alone strands long runs on old versions |
| DBOS | Fencing by ownership token; enqueue inside the caller's transaction; idempotency keys on inbound messages | The mechanics a home-grown engine most often gets wrong |
| Absurd | Engine logic as stored functions with thin pull workers | The closest published blueprint for a small Postgres engine |

Three further ideas come from gaps, not features. No system examined declares compensation next to the step it undoes, although each engine already knows which steps completed. None offers "poll until" as a property of an external step, which a `not_before` timestamp on a re-queued row provides cheaply. And none stores a full AI transcript, prompt version and cost against the step attempt; the AWS agent step returns only final text, discarding tool use and intermediate turns.

## What is unverified or rests on inference

**Unresolved conflicts in sources.** Whether the open-source Conductor server implements `WAIT_FOR_WEBHOOK` and inbound webhooks is unresolved: a docs page says yes, while the `TaskType` enum, a repository tree listing and Orkes' comparison page say no. Treat it as Orkes-only. The Orkes comparison page is stale in places, so its Orkes-only claims are "at least historically". Conductor docs disagree on whether Postgres locking is production-supported. AWS pages disagree on heartbeat-timeout error naming and on the default task timeout. Whether Temporal's Cloud Run Serverless Workers work against a self-hosted server is unresolved. LocalStack's current licence terms were reported inconsistently.

**Dates and versions not confirmed.** Conductor release years are inferred from the page footer. The three DSL patch releases share one publication date (31 July 2026), so their true cut dates are unknown; whether v1.1.0 shipped on 13 November 2026 is unknown. The DBOS Go initial release date, the dates of v1.1 to v1.8 and the latest tag are unverified, and no feature-by-feature parity table against Python and TypeScript exists. Whether the AWS AgentCore harness step left preview, whether Temporal's Upgrade on Continue-as-New reached general availability, and whether Cloud Run worker pools are generally available were not confirmed.

**Stated from prior knowledge or search snippets, not fetched.** Several Temporal details (task queue mechanics, asynchronous activity completion, timers, `GetVersion`, cancellation, saga sample, schedules, retention) were not re-read. Most Temporal agent-integration facts, the March 2026 AWS AgentCore SDK announcement, River Pro pricing, all Restate facts, Hatchet's architecture, Lemline's licence and the 2022 CNCF adopter list come from snippets or secondary sources. Temporal criticisms come from undated blogs.

**Inferences, not quoted statements.** That Conductor and self-hosted Temporal cannot scale to zero. That ASL has no schema validation (concluded from absence). That the ASL polling loop and saga patterns are standard practice. All four step-kind encodings in the DSL and the 40-line human approval estimate. That task definitions in Conductor are not snapshotted with the workflow. The entire Cloud Run doorbell, sweep and outbox pattern; Cloud Tasks, Pub/Sub and Cloud Scheduler guarantees were not read from primary documentation. The fencing and lease design rules, which rest on DBOS's v1.6.0 change and practitioner posts, not an authoritative treatment. The Temporal Cloud cost illustration assumed about 20 actions per run, an unsourced figure, and is not relied on above.

**Not researched.** No cost comparison between one always-on small Cloud Run instance and the Cloud Tasks plus Scheduler pattern. No case study of any of these libraries on scale-to-zero Cloud Run with Cloud SQL. No published DSL interpreter on DBOS Go. No named production adopter of DSL 1.x. BPMN, the obvious benchmark for native human tasks, was not compared directly. Conductor's Postgres schema, River internals, and the DSL's conformance test coverage were not read.

## Conclusion

The four systems converge on one runtime shape even though they disagree on notation: a step that is parked under an unguessable handle until a worker, a person or a third party returns it, with a lease clock and a deadline clock running separately. Human approval and external callback are the same state everywhere examined, differing only in who may complete the step and how they are authenticated. That convergence is the most useful finding for a new engine, because it means the four step kinds are a difference of contract, not of mechanism. The engine can be one small state machine; the definition is where the opinion goes.

It also reframes the standards question. What the Serverless Workflow project offers today is closer to a vocabulary, a JSON Schema and a Go parser than to a set of interchangeable runtimes, and the properties this engine cares about most, an immutable prompt version, a cost budget, a role and an escalation chain, are absent from every system surveyed, including the commercial ones. Those are the parts worth designing carefully and owning. The decision most worth revisiting is not the contract but the substrate: a single always-on instance or a generally available Temporal serverless worker would each change the build-versus-adopt arithmetic, and the cost comparison between a minimum instance and the doorbell-and-sweep pattern is the first number to obtain before committing.
