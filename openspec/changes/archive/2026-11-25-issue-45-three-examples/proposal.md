## Why

The website shows three worked examples: a bug at 2am, a support inbox and a vulnerable library. Only the support inbox exists in `examples/`, and no test runs it, so an example can stop working without anyone noticing. The first tagged version promises that all three run end to end with no key.

Closes issue 45.

## What Changes

- Two new workflows, `examples/bug-at-2am` and `examples/vulnerable-library`, beside `examples/support-inbox`.
- A test runs each of the three files exactly as it is written, with a scripted model and stand-in third parties, down every path the workflow has.
- Each example's README says what the test proves and what has not been run for real.

Nothing in the engine changes.

Out of scope: running the two new examples with a real model; tools for their agents; handlers that really open an issue, deploy or open a pull request; ranking several services against each other, which needs fan-out; requiring a written reason to override an assessment.

## Capabilities

### New Capabilities

- `examples`: the workflows in `examples/` that Weir promises will publish and run as written, and what each one does on each of its paths.

### Modified Capabilities

None.

## Impact

- **Code:** `engine/examples_test.go` only. No engine, API or database change.
- **Dependencies:** none.
- **Rules that must not be weakened:** none is touched.
- **Not proven here:** the two new examples against a real model or a real third party, and the commands in their READMEs.
