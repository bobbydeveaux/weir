# model-providers Specification

## Purpose
Weir reaches a model through an adapter for its provider. This says which providers are built, what each can and cannot do, how a step is sent to one, and what happens when a provider cannot give a step the guarantees it needs.

## Requirements

### Requirement: The runner is chosen by a setting
`WEIR_RUNNER` SHALL choose how `ai` and `decide` steps run. `cli`, the default, runs them through a provider's command line. `fake` returns scripted answers, for tests and examples. `api` is a name the setting accepts, and a server set to it SHALL refuse to start, saying that runner is not built. Any other value SHALL be refused when the settings are read.

#### Scenario: Nothing is set
- **WHEN** the server starts with no `WEIR_RUNNER`
- **THEN** it uses the `cli` runner

#### Scenario: A runner nobody has heard of
- **WHEN** `WEIR_RUNNER` is `magic`
- **THEN** the settings are refused, and the error names `magic`

### Requirement: Claude runs steps through its command line
The Claude adapter SHALL run each step as one call to the Claude command line, signed in with whatever credential the command line finds. It SHALL pass the step's schema and budget, read the structured answer, and report the provider, the model that answered, the tokens in and out, and the cost the command line reports.

#### Scenario: A call comes back
- **WHEN** Claude's command line answers a step with a structured result, a cost of 0.0123 dollars and its token counts
- **THEN** the result is the step's output
- **AND** the usage names the provider `claude`, the model that answered, the tokens and that cost

### Requirement: Codex runs steps through its command line
The Codex adapter SHALL run each step as one call to `codex exec`, with its shell, browser, computer-use, plugin and multi-agent features switched off, in a read-only sandbox, ignoring the user's configuration and rules, and without saving the session. It SHALL read token counts from the command line's events.

#### Scenario: Codex answers a step
- **WHEN** Codex answers a step using 1,200 tokens in and 40 out
- **THEN** the answer is the step's output
- **AND** the usage names the provider `codex` and those token counts

### Requirement: A Codex call's cost is an estimate
Codex reports no cost, so the adapter SHALL work the cost out from the call's tokens and the two prices it is given, and SHALL mark it as estimated. The cost is the tokens in at the price in plus the tokens out at the price out. A call that carries no budget SHALL run without prices, and is then recorded with a cost of zero.

#### Scenario: Prices are given
- **WHEN** Codex uses 1,200 tokens in and 40 out, with prices of 2 and 10 dollars a million
- **THEN** the cost is 0.0028 dollars, marked as estimated

#### Scenario: No prices and no budget
- **WHEN** a call with no budget, such as a decision, is sent to Codex on a server with no prices set
- **THEN** the call runs
- **AND** its cost is zero, marked as estimated

### Requirement: A provider that cannot give a guarantee refuses the step
An adapter SHALL refuse a step it cannot run with the guarantees the step asks for. It SHALL NOT run the step without them. Codex SHALL refuse a step that lists tools, documents to read, `writes`, a `workspace` or a `shell`. A decision model SHALL refuse an ai step. The refusal SHALL fail the attempt with a message saying what the provider cannot do.

#### Scenario: A Codex step with tools
- **WHEN** a step that lists a tool is sent to Codex
- **THEN** the step is refused, saying the Codex adapter cannot offer tools
- **AND** the Codex command line is not started

#### Scenario: A decision model is asked to run an agent
- **WHEN** an ai step is sent to Jev
- **THEN** it is refused

### Requirement: Steps are routed to a provider by tier
With the `cli` runner, a step with `tier: codex` SHALL be run by Codex, and every other step by Claude. This holds for an agent run beside the worker. One workflow can so have one model write a change and another review it.

#### Scenario: A write step and a review step
- **WHEN** one step names no tier and another names a tier that has a runner of its own
- **THEN** the first goes to the default runner and the second to that tier's runner

### Requirement: A tier's model is a setting
`WEIR_CLAUDE_MODELS` SHALL map a tier's name to the model Claude uses for it, as `small=…,large=…`. With nothing set, `small` is `haiku` and `large` is `opus`. A step whose tier is not listed SHALL use the model its template names, or the command line's default. A decide step that names no tier SHALL use `haiku`. `WEIR_CODEX_MODEL` SHALL name the model for Codex.

#### Scenario: A tier with a model
- **WHEN** the tier `small` is mapped to `claude-haiku-5-5` and a step names `tier: small`
- **THEN** the step asks for `claude-haiku-5-5`

#### Scenario: A template's model
- **WHEN** a step names no tier and its template names `claude-sonnet-5-5`
- **THEN** the step asks for `claude-sonnet-5-5`

### Requirement: Claude can be sent to Bedrock, Vertex AI or a gateway by settings
`WEIR_AGENT_ENV` SHALL name environment variables to let through to the model's command line, which otherwise sees almost none of the server's. That is how Claude is sent to Amazon Bedrock, Vertex AI or an AI gateway, on the machine's own identity. `WEIR_CLAUDE_MODEL_PREFIX` SHALL be put in front of a model name that begins `claude-`, for a cloud that serves Claude under its own names. A short alias, and a name that already has the prefix, SHALL be left alone.

#### Scenario: Bedrock in Europe
- **WHEN** the prefix is `eu.anthropic.` and a step's model is `claude-haiku-5-5`
- **THEN** the command line is asked for `eu.anthropic.claude-haiku-5-5`

#### Scenario: Only what is named gets through
- **WHEN** `WEIR_AGENT_ENV` names `CLAUDE_CODE_USE_BEDROCK`
- **THEN** the command line is given that variable
- **AND** it is given no other variable of the server's beyond the short fixed list

### Requirement: A command line's own error is reported
When a command line fails, the attempt SHALL fail with the provider's own account of why: the error and its kind that Claude's output reports, the message in Codex's event stream, or part of what the program printed when it exited badly. What the call cost before it failed SHALL still be reported. A call that returns no structured answer SHALL be a failure.

#### Scenario: Claude stops at its budget
- **WHEN** Claude's command line reports the error `error_max_budget_usd` after spending 5 dollars
- **THEN** the call fails with that error in its message
- **AND** the usage still shows a cost of 5 dollars

#### Scenario: Codex has no such model
- **WHEN** Codex's events report "model not available"
- **THEN** the call fails with that message

### Requirement: A provider that is not set up says so
A step that names a provider the server has no credentials for SHALL get an error that names the provider and the setting to set. The server SHALL NOT send the step to a different provider in its place. A server whose `WEIR_DECIDE_PROVIDER` names something that is not a provider SHALL refuse to start.

#### Scenario: Gemini Decisions has no key
- **WHEN** a decide step names `provider: gemini-decision` and `WEIR_GEMINI_API_KEY` is not set
- **THEN** the call fails with a message that names `WEIR_GEMINI_API_KEY`

### Requirement: Codex's prices are settings
`WEIR_CODEX_USD_PER_MTOK_IN` and `WEIR_CODEX_USD_PER_MTOK_OUT` SHALL give the price of a million tokens in and out on Codex, in US dollars. A setting that is not set gives no price. A value that is not a number, or is less than zero, SHALL be refused when the settings are read, with an error that names the setting.

#### Scenario: Both prices are set
- **WHEN** the settings are `1.25` and `10`
- **THEN** Codex is given a price of 1.25 dollars a million tokens in and 10 dollars a million out

#### Scenario: A price that is not a number
- **WHEN** `WEIR_CODEX_USD_PER_MTOK_OUT` is `cheap`
- **THEN** the settings are refused, and the error names `WEIR_CODEX_USD_PER_MTOK_OUT`

#### Scenario: Nothing is set
- **WHEN** neither setting is set
- **THEN** the settings are read, and Codex has no prices

### Requirement: Codex refuses a budget it cannot enforce
Without both prices the Codex adapter cannot tell what a call costs. It SHALL then refuse a call that carries a budget, before the command line is started, with a message that names the two price settings. The step SHALL fail with the error type `budget_unenforceable` and SHALL NOT be tried again.

#### Scenario: A step with a budget and no prices
- **WHEN** a step with `budget: { usd: 1 }` and `retry: { attempts: 3 }` is sent to Codex on a server with no prices set
- **THEN** the step fails once with the error type `budget_unenforceable`
- **AND** the message names `WEIR_CODEX_USD_PER_MTOK_IN` and `WEIR_CODEX_USD_PER_MTOK_OUT`
- **AND** the Codex command line is not started

#### Scenario: Only one price is set
- **WHEN** a call with a budget is sent to Codex with a price for tokens in and none for tokens out
- **THEN** the call is refused in the same way

### Requirement: A Codex call is held to its budget only after it ends
The Codex command line has no spending limit, so a Codex call SHALL run to its end whatever it costs: the adapter MUST NOT be relied on to stop it part-way. With prices set, a step whose estimated cost is more than its budget SHALL fail with the error type `budget_exceeded` once the call has ended, and the whole estimate SHALL be added to what the run has spent.

#### Scenario: The estimate passes the budget
- **WHEN** a Codex step with a budget of 1 dollar uses a million tokens in, at a price of 2 dollars a million
- **THEN** the command line is run once, to its end
- **AND** the step fails with the error type `budget_exceeded`
- **AND** 2 dollars is added to what the run has spent
