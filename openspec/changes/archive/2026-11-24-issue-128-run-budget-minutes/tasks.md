## 1. Code

- [x] 1.1 `Unsupported` in `spec/definition.go` reports `budget.minutes` when the definition's top-level budget sets minutes above zero, and never a step's
- [x] 1.2 Correct the comment in `spec/definition.go` that said triggers and hooks are not acted on
- [x] 1.3 The console's page for a workflow shows a run's `budget.minutes` as accepted and not enforced, and stops showing triggers and hooks under that heading

## 2. Tests

- [x] 2.1 In `spec/`: a top-level `budget.minutes` is reported with the entries that were reported before; a budget in dollars alone and a step's `budget.minutes` are not
- [x] 2.2 In `api/`, against Postgres: the reply to `POST /v1/definitions` holds `budget.minutes` under `not_yet_enforced`

## 3. Specs and docs

- [x] 3.1 `proof.md` for `definitions`, `ai-steps`, `runs` and `http-api`
- [x] 3.2 `docs/build/02-epics.md` and `README.md`
- [x] 3.3 `make check` and `make spec`
