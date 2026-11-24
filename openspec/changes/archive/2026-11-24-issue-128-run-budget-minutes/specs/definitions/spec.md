## MODIFIED Requirements

### Requirement: Publishing says what it accepts but does not enforce
A definition MAY set `tiers`, `minutes` in its top-level `budget`, a step's `hooks`, a step's `sandbox`, and tools under `tools.approve`. Weir accepts these and does not act on them. The reply to a publish SHALL list each one that is set, as `not_yet_enforced`, so that a publisher is never led to believe it is in force. A step's own `budget.minutes` is enforced and MUST NOT be listed.

#### Scenario: A definition sets a sandbox
- **WHEN** a definition with `sandbox` on the step `fix` is published
- **THEN** it is published
- **AND** the reply's `not_yet_enforced` holds `steps.fix.sandbox`

#### Scenario: A definition sets a time limit on its runs
- **WHEN** a definition with a top-level `budget: { usd: 5, minutes: 30 }` is published
- **THEN** it is published
- **AND** the reply's `not_yet_enforced` holds `budget.minutes`

#### Scenario: A definition sets a budget in dollars alone
- **WHEN** a definition with a top-level `budget: { usd: 5 }` is published
- **THEN** the reply's `not_yet_enforced` does not hold `budget.minutes`

#### Scenario: A step sets a time limit on its agent
- **WHEN** a definition whose `ai` step `fix` sets `budget: { usd: 2, minutes: 10 }` is published
- **THEN** the reply's `not_yet_enforced` holds neither `budget.minutes` nor anything about the budget of `fix`
