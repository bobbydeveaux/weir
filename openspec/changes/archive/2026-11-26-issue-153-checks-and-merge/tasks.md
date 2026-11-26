## 1. The engine

- [x] 1.1 Add `visit` to each step's entry in an expression's scope
- [x] 1.2 Give a handler a "not yet" answer that carries a delay
- [x] 1.3 Queue a deterministic step again on that answer, through the path a poll that is not done takes, keeping its attempts, in one transaction
- [x] 1.4 Stop at the step's `timeout` with the error type `timeout`

## 2. GitHub

- [x] 2.1 Token needs for reading checks, statuses and actions, and for merging; name a permission the App lacks
- [x] 2.2 `github.checks`: read the head commit's checks and whether each is required, from GitHub's GraphQL API; `count: required` or `all`, and named checks; wait while any is unfinished; report `state`, `head_sha`, `mergeable_state`, `failed` and `other`
- [x] 2.2a With `count: all`, wait for `settle` before reporting `none`
- [x] 2.2b Poll with backoff, and honour the rate-limit headers
- [x] 2.3 For each failed check, the end of its log and `fails_on_base`; cap the output
- [x] 2.4 `allow_merge` on the connection, false unless set; both merge handlers refuse without it
- [x] 2.4a `github.merge_pull_request`: `method` required, the pin to `head_sha`, `allow_no_checks`, `merged: false` with a reason, and the checks seen recorded in the output
- [x] 2.4b `github.enable_auto_merge`: turn on GitHub's auto-merge with the given method; `enabled: false` with GitHub's reason
- [x] 2.5 `github.commit` with `update: true`: a further commit on a branch that exists, never forced

## 2b. Definitions

- [x] 2b.1 Publishing refuses a `github.checks` step without a `timeout` or with a `count` that is not one of the two, and a merge step without a `method`
- [x] 2b.2 Publishing warns of a loop that no rule bounds with `visit`

## 3. Tests

- [x] 3.1 A run that goes round a loop three times and is stopped by `steps.<id>.visit`
- [x] 3.2 A handler that is ready the third time, one that is never ready, and one with no timeout
- [x] 3.3 `github.checks` against a stand-in: required checks passed, running, failed and skipped; a named check skipped; an optional check failing; nothing required; `count: all` on a repository with no rules, failing, cancelled, and before and after `settle`; a long log; a log that can't be read; both values of `fails_on_base`; backoff and the rate limit
- [x] 3.4 `github.merge_pull_request` against a stand-in: each method, each refusal (including a missing review and a connection without `allow_merge`), and run twice; `github.enable_auto_merge` enabled and refused
- [x] 3.5 `github.commit` with `update: true`: a second commit, the same files twice, and a branch that is not there
- [x] 3.6 The permissions each new call asks for, and the error for one the App lacks
- [ ] 3.7 An end-to-end run: commit, checks fail, a second commit with `update: true`, checks pass, squash merge. Not done: each part is tested on its own, and no one test runs them as one workflow

## 4. Finish

- [x] 4.1 Update `proof.md` for `github`, `deterministic-steps`, `expressions`, and `definitions`
- [x] 4.2 Update `docs/build/02-epics.md` and `README.md`, including that a check's log is text from outside
- [x] 4.3 Archive the change
- [x] 4.4 Run `make check` and `make spec`
