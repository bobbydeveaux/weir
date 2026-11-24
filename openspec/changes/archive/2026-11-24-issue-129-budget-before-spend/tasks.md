## 1. Engine

- [x] 1.1 `Claim` tells the worker what the step's earlier attempts spent and what the run has left, in the same transaction

## 2. Runner

- [x] 2.1 `runner.ErrBudget`, returned by the Claude adapter when the command line reports `error_max_budget_usd`, however it exited
- [x] 2.2 The scripted runner records what each call was allowed, and can script a cost and a failure for each call

## 3. Worker

- [x] 3.1 A call is allowed the lesser of what the step and the run have left
- [x] 3.2 With less than a cent left, no call is made and the step fails with `budget_exceeded`
- [x] 3.3 A call that fails at its limit fails the step with `budget_exceeded` and is not tried again
- [x] 3.4 Confirm that the limit reaches an agent in a container of its own

## 4. Finish

- [x] 4.1 Tests, against a real Postgres, that fail without the change
- [x] 4.2 `proof.md` for `runs`, `ai-steps`, `model-providers` and `isolated-agents`
- [x] 4.3 The to-do list and the docs
- [x] 4.4 `make check` and `make spec`
