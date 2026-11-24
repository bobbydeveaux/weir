## ADDED Requirements

### Requirement: Claude says when it stopped a call at its limit
When Claude's command line reports a result of the kind `error_max_budget_usd`, the Claude adapter SHALL report the failure as a call stopped at its limit, so that the worker can tell it from any other failure, whether the command line exited cleanly or badly. The cost SHALL still be reported. Any other error the command line reports SHALL NOT be reported this way.

#### Scenario: The command line stops at its limit
- **WHEN** Claude's command line reports `error_max_budget_usd` after spending 5 dollars
- **THEN** the call fails as one stopped at its limit, and the usage shows a cost of 5 dollars

#### Scenario: The command line stops at its limit and exits badly
- **WHEN** the command line prints the same result and exits with status 1
- **THEN** the call still fails as one stopped at its limit

#### Scenario: Another error
- **WHEN** the command line reports `error_during_execution`
- **THEN** the call fails, and not as one stopped at its limit

## MODIFIED Requirements

### Requirement: Claude runs steps through its command line
The Claude adapter SHALL run each step as one call to the Claude command line, signed in with whatever credential the command line finds. It SHALL pass the step's schema and the most the call may spend, read the structured answer, and report the provider, the model that answered, the tokens in and out, and the cost the command line reports.

#### Scenario: A call comes back
- **WHEN** Claude's command line answers a step with a structured result, a cost of 0.0123 dollars and its token counts
- **THEN** the result is the step's output
- **AND** the usage names the provider `claude`, the model that answered, the tokens and that cost
