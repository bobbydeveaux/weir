## MODIFIED Requirements

### Requirement: A Codex call's cost is an estimate
Codex reports no cost, so the adapter SHALL work the cost out from the call's tokens and the two prices it is given, and SHALL mark it as estimated. The cost is the tokens in at the price in plus the tokens out at the price out. A call that carries no budget SHALL run without prices, and is then recorded with a cost of zero.

#### Scenario: Prices are given
- **WHEN** Codex uses 1,200 tokens in and 40 out, with prices of 2 and 10 dollars a million
- **THEN** the cost is 0.0028 dollars, marked as estimated

#### Scenario: No prices and no budget
- **WHEN** a call with no budget, such as a decision, is sent to Codex on a server with no prices set
- **THEN** the call runs
- **AND** its cost is zero, marked as estimated

## ADDED Requirements

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
