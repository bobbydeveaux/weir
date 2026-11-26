## 1. The engine

- [ ] 1.1 Add `visit` to each step's entry in an expression's scope
- [ ] 1.2 Give a handler a "not yet" answer that carries a delay
- [ ] 1.3 Queue a deterministic step again on that answer, through the path a poll that is not done takes, keeping its attempts, in one transaction
- [ ] 1.4 Stop at the step's `timeout` with the error type `timeout`

## 2. GitHub

- [ ] 2.1 Token needs for reading checks, statuses and actions, and for merging; name a permission the App lacks
- [ ] 2.2 `github.checks`: read check runs and statuses on the head commit, wait while any is unfinished, and report `state`, `head_sha` and `failed`
- [ ] 2.3 For each failed check, the end of its log and `fails_on_base`; cap the output
- [ ] 2.4 `github.merge_pull_request`: the three methods, the pin to `head_sha`, `allow_no_checks`, and `merged: false` with a reason
- [ ] 2.5 `github.commit` with `update: true`: a further commit on a branch that exists, never forced

## 3. Tests

- [ ] 3.1 A run that goes round a loop three times and is stopped by `steps.<id>.visit`
- [ ] 3.2 A handler that is ready the third time, one that is never ready, and one with no timeout
- [ ] 3.3 `github.checks` against a stand-in: passed, running, failed, none, skipped and cancelled, a long log, a log that cannot be read, and both values of `fails_on_base`
- [ ] 3.4 `github.merge_pull_request` against a stand-in: each method, each refusal, and run twice
- [ ] 3.5 `github.commit` with `update: true`: a second commit, the same files twice, and a branch that is not there
- [ ] 3.6 The permissions each new call asks for, and the error for one the App lacks
- [ ] 3.7 An end-to-end run: commit, checks fail, a second commit with `update: true`, checks pass, squash merge

## 4. Finish

- [ ] 4.1 Update `proof.md` for `github`, `deterministic-steps` and `expressions`
- [ ] 4.2 Update `docs/build/02-epics.md` and `README.md`, including that a check's log is text from outside
- [ ] 4.3 Archive the change
- [ ] 4.4 Run `make check` and `make spec`
