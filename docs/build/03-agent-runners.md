# Agent runners and providers

Decision, 16 November 2026.

Weir runs `ai` and `decide` steps through a **runner**, and the runner drives a **provider**. Both are chosen by configuration. The first version uses the command-line runner with Claude on a long-lived OAuth token, as the earlier projects do. Other command lines (Codex, Gemini, Grok and others) plug in as providers, as they did in an earlier project. The engine does not care which is in use.

## Two settings

```yaml
runner: cli                  # cli | api | fake

tiers:
  small:  { provider: claude, model: haiku,  effort: low }
  large:  { provider: claude, model: opus,   effort: high }
  review: { provider: codex }               # a second opinion from a different model
```

| Setting | Chooses | Values |
| --- | --- | --- |
| `runner` | How a step is executed | `cli` starts a command line per attempt. `api` calls a provider's API with a key. `fake` returns scripted answers for tests |
| `provider` | Which model family does the work | `claude`, `codex`, `gemini`, and others as adapters are added |

The runner is set per workspace. The provider is set per tier, so one workflow can write a fix with one model and review it with another. An earlier operator project used exactly that pattern for its reviewer.

## What every provider reports

Each runner returns the same outcome, so the audit record and the budget checks are identical whichever provider ran.

| Outcome | Notes |
| --- | --- |
| The structured output | Validated by Weir against the step's schema, whatever the provider claims |
| The transcript | Stored against the attempt |
| Tokens in and out | From the provider's JSON output |
| Cost | From the provider's JSON output where it reports one; otherwise worked out from tokens and a price table, and marked as estimated |
| Every tool call | Recorded by Weir's tool proxy, not taken from the provider |

**Cost with the Claude command line.** Run with `--output-format json`, it reports the cost and token usage of every run. Weir records that figure and enforces the budget against it. On a subscription token the figure is what the run would have cost at API prices, not an amount charged, so it is a sound basis for a budget and for comparing runs.

## Providers

A provider adapter is small: how to pass the prompt, how to name the model, how to ask for structured output, how to restrict tools, and how to read cost and usage from the output. An earlier project's adapters were 160 to 420 lines each.

Checked on this machine on 16 November 2026 by reading each command's help:

| Provider | Non-interactive command | Structured output | Restricting tools | Status |
| --- | --- | --- | --- | --- |
| Claude | `claude -p --output-format json` | `--json-schema` | `--allowedTools`, and a strict MCP configuration | Installed. First adapter |
| Codex | `codex exec --json` | `--output-schema <file>` | Not checked yet | Installed. Second adapter |
| Gemini | `gemini <prompt>` (the `-p` flag is deprecated) | Not found in its help | `--allowed-tools`, `--allowed-mcp-server-names`, and an approval mode | Installed. Third adapter |
| Grok | Unknown | Unknown | Unknown | Not installed here. An earlier project did not use a Grok command line: it called the xAI API directly |
| Antigravity (`agy`) | Unknown | Unknown | Unknown | Not installed here, so not verified |
| Ollama | Local models | Unknown | Unknown | Installed. An earlier project had an adapter |

Each adapter's flags are confirmed against the installed version when it is built, and the version is pinned.

## Capabilities, and what happens when one is missing

The command lines do not all offer the same controls. Each adapter declares what it can enforce, and Weir fills the gaps or refuses.

| Guarantee | If the provider supports it | If it does not |
| --- | --- | --- |
| Output matches the schema | Weir passes the schema, and validates the result again | Weir validates the result itself. A failure is a failed attempt |
| Only listed tools may be called | Weir passes the allow-list and exposes only its own tool server | The step runs only if it lists no write tools, or the definition is refused when published |
| Cost is recorded | Read from the output | Worked out from tokens and a price table, marked as estimated |
| The agent holds no credentials | A clean environment with only its own token | The same. This never depends on the provider |

The point of the last column is that a weaker provider can still be used safely for the steps that suit it, such as a read-only review, without weakening the guarantees for every step.

## How a command-line runner is locked down

The earlier projects ran command lines in two ways. Three of them ran every provider in its auto-approve mode (`--dangerously-skip-permissions`, `--full-auto`, `--yolo`). Two others kept permission checks on. Weir copies the second pair.

| Rule | Why |
| --- | --- |
| Permission checks stay on, with only listed tools allowed | Auto-approve makes the allow-list advisory |
| The agent's only tool server is Weir itself | Every tool call then passes the allow-list, gets its credential from Weir, and is recorded |
| Built-in tools are off unless the step lists them | A step that should only read cannot write |
| User and project settings are ignored | A repository being worked on cannot change the agent's rules |
| The process gets a clean environment with only the token it needs | In two earlier projects the agent could read every secret the worker held |
| It runs in a throwaway working directory with a time limit | A stuck agent is killed and the attempt fails cleanly |

## Command line or API

| | `cli` runner | `api` runner |
| --- | --- | --- |
| Start-up time per step | A second or two for the process to start | None |
| Stopping at the budget | Checked as usage is reported; the process is killed when the limit is passed | Checked before every model call |
| `decide` steps | Works, but slow for a classifier | Fast |
| What an operator must install | Each provider's command line, at a pinned version | Nothing |
| Credential | That provider's own sign-in or token | An API key |

A `decide` step on the command-line runner pays the start-up time on every decision. That is acceptable for the first version. A fast classifier, such as the one that earlier assistant uses uses, can be added later as another provider.

## A caution for anything beyond personal use

A long-lived OAuth token belongs to one person's subscription. That suits running Weir for yourself. Other people running the open-source engine will supply their own credential, and a hosted service would need API keys. Check each provider's terms before using a subscription token for anything shared.

## What this changes in the plan

- Epic 9 builds the runner interface, the provider adapter interface, the Claude adapter and the `fake` runner.
- Codex and Gemini adapters follow. They are independent of each other and of the engine, which makes them good first contributions.
- Grok and Antigravity adapters wait until their command lines can be checked.
- The `api` runner follows once a key is available.
- The tool proxy in epic 10 is an MCP server that Weir exposes to the command line.

## A decisions API as the backend for `decide` (noted 2026-11-16)

Google announced the Gemini Decisions API on 11 November 2026
([post](https://blog.google/innovation-and-ai/technology/developers-tools/gemini-decisions-api/)).
It is a close match for the `decide` step, and for what Jev did in an earlier production assistant.

What the post says:

- A model (`gemini-decision-1`) that scores a closed set of options in one pass instead of writing an answer.
- Three question types: yes/no, choice (2 to 255 labels, each with a description) and an ordered rubric score.
- Every answer carries a confidence from 0 to 1; a choice also returns a probability for every label.
- Up to 1,000 questions against one shared input (up to 256K tokens; text, images, audio, video, PDFs). The input is billed once.
- $0.10 per million input tokens, nothing for output. 150 to 250 ms end to end for inputs up to 16K tokens.
- Gemini API with an API key from Google AI Studio. Vertex AI is "coming soon". The sample uses `v1beta`.

What this means for Weir:

- It needs the `api` runner. There is no sign it can be reached through a command line or an OAuth sign-in, so it would be the first real use of an API key.
- `decide` keeps its contract (labels, confidence, default). Only the provider behind it changes. The threshold and default stay in Weir, because the API has no fallback of its own.
- The many-questions-per-call shape suits the trigger ladder: one call can ask "does this event belong to workflow A, B, C…" for every candidate at once.
- The Claude small model stays as the default so that Weir works with one sign-in and nothing leaves for a second provider unless configured.

Not yet known, to check before building: the response field names, rate limits, whether it is preview or generally available,
regions, and whether the confidence is well calibrated on our own labels (the post's claim is the vendor's, so shadow mode should test it).

### Jev next to it

That assistant uses Jev, from TypeSafe ([post](https://typesafe.ai/blog/introducing-system-one-models-and-jev)). The two are the same kind of thing:
a model that scores a closed set of options in one pass and cannot write free text. Figures below are each vendor's own.

| | Jev (TypeSafe) | Gemini Decision 1 (Google) |
|---|---|---|
| Labels per question | Up to 255 | 2 to 255 |
| Confidence | "Calibrated probabilities and confidence scores" with every answer | Confidence 0 to 1, plus a probability per label |
| Questions per call | Several; no limit stated | Up to 1,000 |
| Input | Text and structured state; no images yet | Text, images, audio, video, PDFs; up to 256K tokens |
| Price per million input tokens | $0.042 (the post says it cannot prove this is not subsidised) | $0.10 |
| Output tokens | Free | Free |
| Latency, end to end | 70 to 500 ms | 150 to 250 ms (inputs up to 16K tokens) |
| Access | Early access, waitlist | Gemini API key; a no-cost tier for prototyping; Vertex AI "coming soon" |
| Hosted where | US West Coast, per the post | Not stated |

Decided 2026-11-16: Jev answers decisions for now, Gemini Decisions is supported beside it, and the workflow chooses (`provider:` on the `decide` step). Weir does not pick one. `decide` gets a provider setting, and a "decision model" is one kind of provider beside the small language model.
Jev goes in first, because there is already working code and an account for it; Gemini Decision 1 is the second, and the one most people who install Weir can get a key for.
Shadow mode (run two providers on the same inputs and compare labels and confidence) is how the choice gets made on real data rather than on vendor charts.

One provider for both would be a mistake: the request shapes are nearly the same today, and nothing says they stay that way.

### As built

A `decide` step may carry `provider: jev | gemini-decision | claude | codex`. A step that names none uses the server's default:
`WEIR_DECIDE_PROVIDER` if set, otherwise Jev when `WEIR_TYPESAFE_KEY` is set, otherwise Claude's small model.
A step that names a provider with no key falls back to its `default` label, with the reason recorded; it is never silently sent to another provider.

Both clients are plain HTTP, written from the vendors' documentation:

| | Jev | Gemini Decisions |
|---|---|---|
| Request | `POST https://api.typesafe.ai/v1/systemone` | `POST https://generativelanguage.googleapis.com/v1beta/decisions` |
| Key | `Authorization: Bearer` from `WEIR_TYPESAFE_KEY` | `x-goog-api-key` from `WEIR_GEMINI_API_KEY` |
| Model | `jev-latest` | `gemini-decision-1` |
| The text to judge | `state` | `input` |

Labels may be described with an optional `describe:` map on the step (label to meaning). Jev and Gemini Decisions receive each description as that option's criterion; Claude and Codex get them as lines in their instructions.

Not yet done: a live call to either (no key was available when they were written), and asking many questions in one call for triggers.

## How Weir signs in to a model provider (decided 2026-11-16)

A runner needs a credential. Weir supports three kinds, and the third is the one the platform is built around:

| Kind | What it is | Where it fits |
|---|---|---|
| OAuth token | The long-lived token a command line signs in with (`claude`, `codex`) | A laptop, a small team, the first version. Built. |
| API key | A key from the provider, held in the environment or a secret store | Jev, Gemini Decisions, and any provider with no other way in. Built for the decision models. |
| Workload identity | No stored secret. The workload proves who it is to the cloud (Google Workload Identity Federation, AWS IAM roles, Azure managed identity) and is given a short-lived token | The default for the hosted service and for an enterprise install. Not built yet. |

Workload identity is what lets Weir reach the models a company already has under its cloud contract:

- **Vertex AI** (Claude, Gemini) through Google Workload Identity Federation.
- **Amazon Bedrock** (Claude and others) through an IAM role.
- **Azure AI Foundry** through a managed identity.

An earlier system already works this way: its small decision model (Claude Haiku) is reached through Google Workload Identity Federation, and it falls back to that whenever no OAuth token is supplied.

The rule for choosing, in order:

1. If the step or the server names a provider, use the credential configured for that provider.
2. If an OAuth token is present and the step runs through a command line, use it.
3. Otherwise use workload identity, when the machine has one.
4. Otherwise an API key.
5. With none of these, the step fails with a message that says which credential is missing. It is never sent to a different provider.

What this needs, as epics:

- An `api` runner for `ai` steps that calls a model directly, with its own tool loop against Weir's tool proxy. The command-line runners lean on the CLI's own loop; a direct call cannot.
- Three back ends for it: Vertex AI, Bedrock, Azure AI Foundry, each taking its token from the cloud's own identity library.
- Per-workspace provider settings, so that one install can send one team to Bedrock and another to Vertex AI, each on its own cloud account.
- For the decision models, the same: Claude Haiku on Vertex AI as a `decide` provider beside Jev and Gemini Decisions.

### The short way to Bedrock and Vertex AI (built 2026-11-17)

Claude's own command line can send its requests to Amazon Bedrock or Vertex AI, on the machine's cloud identity, when told to by
environment variables (`CLAUDE_CODE_USE_BEDROCK=1`, `CLAUDE_CODE_USE_VERTEX=1`). An earlier project of the author's runs this way in production. So for Claude,
Weir does not need the direct-call runner to reach a company's cloud contract: it needs to let those variables through to the agent.

Weir starts an agent with almost none of the server's environment, on purpose. Four settings open it up, each chosen by the operator:

| Setting | What it does |
|---|---|
| `WEIR_AGENT_ENV` | Names the variables an agent's command line may see |
| `WEIR_CLAUDE_MODELS` | The model for each tier |
| `WEIR_CLAUDE_MODEL_PREFIX` | Put before a `claude-` model name, as Bedrock's inference profiles need (`eu.anthropic.`) |
| `WEIR_TYPESAFE_URL`, `WEIR_GEMINI_API_URL` | A different address for a decision model |

The same pass-through sends Claude through an AI gateway (`ANTHROPIC_BASE_URL`). Worked examples are in the engine's README.

Not yet run against a real Bedrock, Vertex AI or gateway account. To check when it is: that a cost is still recorded for each call, since budgets depend on it.

What this does not cover, and the direct-call runner still must: other models on Bedrock, Azure AI Foundry, the decision models on a
cloud identity, and different providers for different workspaces (these settings are for the whole install).

**Where the agent runs is a separate question.** Bedrock is where the model is. AgentCore, Fargate or Cloud Run is where the agent's
process is. That project has a runner for each (`local`, `fargate`, `agentcore`), all running the same harness. Weir has only the first:
the worker runs the agent beside itself. A runner that hands an `ai` step to a process hosted elsewhere is its own epic.
