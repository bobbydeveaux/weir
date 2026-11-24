## 1. Settings

- [x] 1.1 Read `WEIR_CODEX_USD_PER_MTOK_IN` and `WEIR_CODEX_USD_PER_MTOK_OUT` in `internal/config`, refusing a value that is not a number of dollars
- [x] 1.2 Pass the prices to the Codex adapter in `cmd/weir/main.go`

## 2. The refusal

- [x] 2.1 The Codex adapter refuses a call with a budget when it has no prices, before starting the command line
- [x] 2.2 The worker fails that step with `budget_unenforceable` and does not try it again

## 3. Tests

- [x] 3.1 `runner/codex_test.go`: the refusal, a call with no budget, and the estimate
- [x] 3.2 `internal/config/config_test.go`: the settings are read, a bad value is refused by name, unset means no prices
- [x] 3.3 `api/codex_budget_test.go`: against Postgres, a Codex step is refused with no prices, and fails with `budget_exceeded` when its estimate passes its budget

## 4. Finish

- [x] 4.1 `proof.md` for `model-providers` and `ai-steps`
- [x] 4.2 `README.md`, `docs/build/03-agent-runners.md` and the to-do list in `docs/build/02-epics.md`
- [x] 4.3 `make check` and `make spec`
