## 1. The engine

- [ ] 1.1 Add `visit` to each step's entry in an expression's scope
- [ ] 1.2 Give a handler a "not yet" answer that carries a delay
- [ ] 1.3 Queue a deterministic step again on that answer, through the path a poll that is not done takes, keeping its attempts, in one transaction
- [ ] 1.4 Stop at the step's `timeout` with the error type `timeout`

## 2. GitHub

- [ ] 2.1 Token needs for reading checks, statuses and actions, and for merging; name a permission the App lacks
- [ ] 2.2 `github.checks`: read the base branch's required checks (branch protection and rulesets) and any named ones; wait while any is unfinished; report `state`, `head_sha`, `mergeable_state`, `failed` and `other`
- [ ] 2.2a Wake a waiting `github.checks` step on `check_run`, `check_suite` and `status` webhooks; poll as the fallback with backoff, and honour the rate-limit headers
- [ ] 2.3 For each failed check, the end of its log and `fails_on_base`; cap the output
- [ ] 2.4 `allow_merge` on the connection, false unless set; both merge handlers refuse without it
- [ ] 2.4a `github.merge_pull_request`: `method` required, the pin to `head_sha`, `allow_no_checks`, `merged: false` with a reason, and the checks seen recorded on the event
- [ ] 2.4b `github.enable_auto_merge`: turn on GitHub's auto-merge with the given method; `enabled: false` with GitHub's reason
- [ ] 2.5 `github.commit` with `update: true`: a further commit on a branch that exists, never forced

## 2b. Definitions and prompts

- [ ] 2b.1 Publishing refuses a `github.checks` step without a `timeout`, and a merge step without a `method`
- [ ] 2b.2 Publishing warns of a loop that no rule bounds with `visit`
- [ ] 2b.3 Handlers mark text from outside; the mark follows values through expressions; a marked value fills a prompt inside an escaped `<untrusted>` block

## 3. Tests

- [ ] 3.1 A run that goes round a loop three times and is stopped by `steps.<id>.visit`
- [ ] 3.2 A handler that is ready the third time, one that is never ready, and one with no timeout
- [ ] 3.3 `github.checks` against a stand-in: required checks passed, running, failed and skipped; an optional check failing; nothing required; a long log; a log that can't be read; both values of `fails_on_base`; woken by a webhook; backoff and the rate limit
- [ ] 3.4 `github.merge_pull_request` against a stand-in: each method, each refusal (including a missing review and a connection without `allow_merge`), and run twice; `github.enable_auto_merge` enabled and refused
- [ ] 3.4a A log containing `</untrusted>` stays inside the block
- [ ] 3.5 `github.commit` with `update: true`: a second commit, the same files twice, and a branch that is not there
- [ ] 3.6 The permissions each new call asks for, and the error for one the App lacks
- [ ] 3.7 An end-to-end run: commit, checks fail, a second commit with `update: true`, checks pass, squash merge

## 4. Finish

- [ ] 4.1 Update `proof.md` for `github`, `deterministic-steps`, `expressions`, `definitions` and `ai-steps`
- [ ] 4.2 Update `docs/build/02-epics.md` and `README.md`, including that a check's log is text from outside
- [ ] 4.3 Archive the change
- [ ] 4.4 Run `make check` and `make spec`
