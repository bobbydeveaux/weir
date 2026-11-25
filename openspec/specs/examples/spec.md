# examples Specification

## Purpose

The workflows in `examples/` are what a new reader runs first, and what the website shows. This spec says what each one does on each of its paths, and that each runs as it is written with a scripted model, so an example cannot stop working unnoticed.

## Requirements

### Requirement: The examples run as they are written, with no key
Each workflow file under `examples/` SHALL publish and run exactly as it is written against a scripted model and stand-in third parties, so that running it needs no model key and costs nothing.

#### Scenario: An example is published and run
- **WHEN** a file under `examples/` is published unchanged, with the templates and connections its README names, and a run of it is started
- **THEN** the run reaches the end of each path described in that example's README

### Requirement: The support inbox sends only a reply that someone else approved
The `support-inbox` example SHALL draft a reply to a message the classifier gives a label other than `other`, hold it for a team lead who is not the requester, and send it once approved. A message labelled `other`, and a draft that is rejected, SHALL be filed.

#### Scenario: A refund request is approved
- **WHEN** a message is labelled `refund`, the requester tries to approve the draft, and then someone else approves it
- **THEN** the requester is refused
- **AND** the run succeeds with the drafted reply addressed to the sender

#### Scenario: Noise
- **WHEN** a message is labelled `other`
- **THEN** the run is filed with no agent called and nothing spent

#### Scenario: A rejected draft
- **WHEN** the draft is rejected
- **THEN** the run is filed and nothing is sent

### Requirement: The bug at 2am is fixed only with approval, and confirmed by the error tracker
The `bug-at-2am` example SHALL start a run for an `errors.new` event whose severity is `error` and for no other. It SHALL send an error the classifier does not confidently label `fix` to a person. Otherwise an agent SHALL write a fix, the run SHALL wait for the on-call engineer, and only after approval SHALL it deploy and ask the error tracker to confirm. A rejected fix SHALL go to a person.

#### Scenario: A warning
- **WHEN** an `errors.new` event arrives with the severity `warning`
- **THEN** no run starts and no model is called

#### Scenario: Fixed, approved and confirmed
- **WHEN** an error is labelled `fix` with confidence, the fix is approved, and the error tracker calls back
- **THEN** the error tracker is not called before the approval
- **AND** the deploy step is given the branch the agent wrote
- **AND** the run succeeds

#### Scenario: The classifier is unsure
- **WHEN** the classifier's confidence is below the step's threshold
- **THEN** the run waits for a person, and no agent runs

#### Scenario: The fix is rejected
- **WHEN** the on-call engineer rejects the fix
- **THEN** the run waits for a person, and the error tracker is never called

### Requirement: The vulnerable library is finished when the scanner says so
The `vulnerable-library` example SHALL start a run for a `scanner.finding` event whose severity is `critical` and for no other. An agent SHALL assess the finding, and the run SHALL wait for an approver. After approval a second agent SHALL make the fix, a pull request step SHALL be given the branch and the assessment, and the run SHALL stay open until the scanner reports the finding resolved. A rejected plan SHALL end the run with no fix.

#### Scenario: A low finding
- **WHEN** a `scanner.finding` event arrives with the severity `low`
- **THEN** no run starts

#### Scenario: The scanner still reports the finding
- **WHEN** the plan is approved and the scanner answers that the finding is open
- **THEN** the run is still running

#### Scenario: The scanner reports it resolved
- **WHEN** the scanner is next asked and answers that the finding is resolved
- **THEN** the run succeeds with the scanner's reply as its output

#### Scenario: The plan is rejected
- **WHEN** the approver rejects the plan
- **THEN** the run ends with no fix made and the scanner never asked
