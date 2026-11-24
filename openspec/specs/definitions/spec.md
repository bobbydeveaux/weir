# definitions Specification

## Purpose
A definition is one workflow, written in YAML. Weir checks it when it is published, stores it as a numbered version that never changes, and starts runs on the version the `live` alias points at.

## Requirements

### Requirement: A definition is YAML and an unknown key is refused
A definition SHALL be written in YAML. Weir MUST refuse a definition that holds a key it does not know, at any level, so that a misspelt key cannot silently switch a rule off.

#### Scenario: A misspelt key
- **WHEN** a definition spells a step's `handler` as `handlr`
- **THEN** the definition is refused before anything is stored

### Requirement: A definition names its contract version, its namespace and its name
A definition MUST set `weir` to `"0.1"`, and a `namespace` and a `name` made of lower-case letters, digits and hyphens, starting with a letter. It MUST have at least one step. Each step MUST have an `id` of lower-case letters, digits and underscores that no other step uses, and a `kind` that is one of `decide`, `ai`, `deterministic`, `human` or `external`.

#### Scenario: Another contract version
- **WHEN** a definition with `weir: "9"` is published
- **THEN** publishing is refused, and the error says what `weir` must be

#### Scenario: A kind that does not exist
- **WHEN** a definition has a step with `kind: magic`
- **THEN** publishing is refused, and the error lists the five kinds

### Requirement: A step uses only the fields of its kind
Publishing SHALL refuse a step that sets a field belonging to another kind, and SHALL name the field and the kind in the error.

#### Scenario: A prompt on a deterministic step
- **WHEN** a deterministic step also sets `prompt`
- **THEN** publishing is refused with `prompt is not a field of a deterministic step`

### Requirement: Publishing reports every problem it finds
When a definition breaks more than one rule, publishing SHALL report all of them in one error, each naming the step and the field it is about. It MUST NOT stop at the first.

#### Scenario: Two things are missing
- **WHEN** an `ai` step is published with neither an output schema nor a budget
- **THEN** publishing is refused
- **AND** the one error mentions both the output schema and the budget

### Requirement: Every reference must resolve when a definition is published
Publishing SHALL refuse a definition that refers to something that does not exist: a `next` rule that names a step not in the definition, a handler or an undo handler that is not registered, a template version that has not been published, or a connection the workspace does not have. A step's output schema MUST be a JSON Schema that compiles. Nothing is stored when publishing is refused.

#### Scenario: A handler nobody has registered
- **WHEN** a deterministic step names the handler `nobody.home`
- **THEN** publishing is refused, and the error says the handler is not registered

#### Scenario: A next rule that goes nowhere
- **WHEN** a `next` rule has `go: nowhere` and no step has that id
- **THEN** publishing is refused, and the error says the step does not exist

#### Scenario: A template that was never published
- **WHEN** a decide step names `missing@1`
- **THEN** publishing is refused as not found

### Requirement: Each publish makes a new numbered version that cannot be changed
Publishing a definition SHALL store it as the next version of its namespace and name, numbered from 1, with the YAML exactly as it was sent and who published it. Publishing the same YAML again SHALL make another version. A published version MUST NOT be changed or deleted afterwards; the database refuses both.

#### Scenario: Publishing twice
- **WHEN** the same definition is published twice
- **THEN** the first is version 1 and the second is version 2

#### Scenario: Someone edits a version in the database
- **WHEN** a published version is updated or deleted with SQL
- **THEN** the database refuses

### Requirement: The live alias points at the newest version
Publishing SHALL move the alias `live` to the version it has just made. A run SHALL start on the version `live` points at, and SHALL stay on that version for its whole life, whatever is published later. `live` is the only alias.

#### Scenario: A first publish
- **WHEN** a definition is published for the first time
- **THEN** its live version is 1
- **AND** a run of it can be started

### Requirement: A template is published as numbered versions
A prompt for an `ai` step and a template for a `decide` step SHALL be published under a key, and each publish SHALL make the next version of that key, numbered from 1. A template MUST have a `user` text; it MAY have a `system` text, a `model` and an `effort`, which MUST be one of `low`, `medium`, `high`, `xhigh` or `max`. A published template version MUST NOT be changed or deleted; the database refuses both. A template belongs to the workspace it was published in.

#### Scenario: Publishing a template
- **WHEN** a template is posted to `/v1/templates/classify` with a `user` text
- **THEN** the reply is 201, with the key and the new version number

#### Scenario: Another workspace's template
- **WHEN** a workspace publishes a definition that names a template only another workspace has
- **THEN** publishing is refused

### Requirement: A model step names a pinned template version
A `decide` step's `template` and an `ai` step's `prompt` MUST be written as `key@version`, such as `fix-incident@7`. Publishing SHALL refuse a reference with no version. A step SHALL always be given the version it names, and never a newer one.

#### Scenario: A template with no version
- **WHEN** a decide step sets `template: route`
- **THEN** publishing is refused, and the error asks for a pinned version

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
