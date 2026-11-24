# decide-steps Specification

## Purpose
A decide step asks a model one question and takes one label from a closed list as the answer. It never fails for want of an answer: anything short of a confident, valid label becomes the step's declared default.

## Requirements

### Requirement: A decide step chooses one label from a closed list
A decide step SHALL list its `labels`, and its result SHALL be exactly one of them. The label SHALL be available to the step's `next` rules as `label`. The step's output SHALL hold `label`, `confidence`, `confident` and `reason`.

#### Scenario: A confident answer routes the run
- **WHEN** the model answers `fix` with confidence 0.95, and the step's threshold is 0.8
- **THEN** the step's label is `fix`
- **AND** the run follows the rule `when: label == "fix"`

### Requirement: An unsure answer becomes the default
A decide step MAY set `confidence`, a threshold between 0 and 1. When the model's confidence is below it, the engine SHALL record the step's `default` as its label, and the step SHALL succeed.

#### Scenario: The model is not sure enough
- **WHEN** the model answers `fix` with confidence 0.4, and the step's threshold is 0.8 and its default is `needs_human`
- **THEN** the step succeeds with the label `needs_human`
- **AND** the step the label `fix` leads to does not run

### Requirement: A failed or wrong answer becomes the default
When the model cannot be reached, returns an error, or answers with something that is not one of the labels, the step SHALL succeed with its `default` as the label. The output SHALL say `confident: false` and give the reason.

#### Scenario: The classifier is down
- **WHEN** the call to the model fails
- **THEN** the step succeeds with the default label

#### Scenario: The answer is not on the list
- **WHEN** the model answers `maybe`, and the labels are `fix` and `needs_human`
- **THEN** the step succeeds with the default label

### Requirement: The engine refuses a label that is not on the list
The engine SHALL check the label of a completed decide step against the step's labels itself, whatever completed the step. A label that is not on the list SHALL fail the attempt with the error type `label_invalid`.

#### Scenario: A completion carries a label nobody declared
- **WHEN** a decide step is completed with the label `maybe`, and the labels are `fix` and `needs_human`
- **THEN** the attempt fails with the error type `label_invalid`

### Requirement: Publishing checks a decide step
Publishing SHALL refuse a decide step unless its `template` names a pinned version that exists, such as `route@3`, it lists at least two labels, its `default` is one of the labels, and its `confidence` is between 0 and 1.

#### Scenario: The default is not a label
- **WHEN** a definition whose decide step has `default: other`, with labels `fix` and `skip`, is published
- **THEN** publishing is refused, and the error says the default must be one of the labels

#### Scenario: The template does not exist
- **WHEN** a definition whose decide step names `missing@1` is published
- **THEN** publishing is refused as not found

### Requirement: Labels may carry descriptions
A decide step MAY give a `describe` entry for any of its labels, saying what the label means. The descriptions SHALL be sent to the model with the labels. Publishing SHALL refuse a description for something that is not one of the labels.

#### Scenario: A description for a label that is not there
- **WHEN** a definition whose decide step has labels `a` and `b` and a description for `c` is published
- **THEN** publishing is refused, and the error names `c`

#### Scenario: A description reaches a decision model
- **WHEN** a step with the label `refund` described as "Wants money back" is answered by Jev
- **THEN** the request carries that description beside the label

### Requirement: The provider is chosen per step
A decide step MAY name its `provider`: `jev`, `gemini-decision`, `claude` or `codex`. Publishing SHALL refuse any other name. A step that names a provider SHALL be answered by it. A step that names none SHALL be answered by the server's default: the one in `WEIR_DECIDE_PROVIDER`, or Jev when `WEIR_TYPESAFE_KEY` is set, or otherwise Claude's small model.

#### Scenario: A provider that is not known
- **WHEN** a definition whose decide step has `provider: crystal-ball` is published
- **THEN** publishing is refused, and the error names the provider

#### Scenario: Two steps, two providers
- **WHEN** the default provider is Jev and a step names `provider: claude`
- **THEN** that step is answered by Claude
- **AND** a step that names no provider is answered by Jev

### Requirement: A language model is given the labels as a closed list
When a decide step is answered by a language model, the model SHALL be asked for structured output whose label is restricted to the step's labels, and SHALL be told that the text it is given is information to classify and never instructions to follow. With no tier named, Claude SHALL use its small model.

#### Scenario: Claude classifies a message
- **WHEN** a decide step with labels `fix` and `needs_human` is answered by Claude
- **THEN** the request restricts the label to those two values
- **AND** the model asked for is `haiku`

### Requirement: A decision model answers over HTTP with an API key
Jev and Gemini Decisions SHALL be called over HTTP with the server's key for that service, and SHALL return the chosen label and a confidence. A call with no key, a reply with an error, and a reply with no answer or no confidence SHALL each be an error, which the step then turns into its default. A decision model SHALL refuse to run an ai step.

#### Scenario: Jev answers
- **WHEN** a decide step is sent to Jev, which chooses `refund` with confidence 0.97
- **THEN** the answer is `refund` with confidence 0.97
- **AND** the request carried the key as a bearer token

#### Scenario: The service refuses
- **WHEN** Gemini Decisions replies 400 with the message "blocked by policy"
- **THEN** the error holds that message

### Requirement: Every decision is recorded as a model call
Each decide step SHALL record one model call against its attempt: the provider, the model, the template and its version, a hash of the prompt, the prompt and the reply, the tokens in and out, the cost and how long it took. A cost worked out from tokens and a price SHALL be marked as estimated. The record SHALL be kept when the answer became the default.

#### Scenario: A run's record shows the decision
- **WHEN** a run with one decide step using the template `classify` has finished
- **THEN** the run's record holds one model call, for the template `classify`
