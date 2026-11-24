## MODIFIED Requirements

### Requirement: Publishing names the parts of a definition that are not acted on
A definition's `tiers` block, `minutes` in its top-level `budget`, a step's `sandbox`, and tools listed under `tools.approve` are not acted on. Publishing SHALL accept them and SHALL list each one in its reply as not yet enforced, so that a publisher is never led to believe they are in force.

#### Scenario: A definition with a tiers block
- **WHEN** a definition with a top-level `tiers` block is published
- **THEN** it is published
- **AND** the reply lists `tiers` under `not_yet_enforced`

#### Scenario: A definition with a time limit on its runs
- **WHEN** a definition with a top-level `budget` that sets `minutes: 30` is published
- **THEN** it is published
- **AND** the reply lists `budget.minutes` under `not_yet_enforced`
