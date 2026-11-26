## Why

A workflow can open a pull request with `github.commit`, and then has no way to finish the job. It cannot find out whether the checks on the pull request passed, it cannot add a second commit when they failed, and it cannot merge. Today a person does all three and tells the run at a human step.

An external step cannot fill the gap. Its `call.path` is a fixed string, and a GitHub App connection refuses any call that is not a GitHub tool or handler.

Closes issue 153.

## What Changes

- **A handler that waits for a pull request's checks.** `github.checks` waits for the checks that count on the pull request's head commit. By default those are the checks GitHub says are required for that pull request. With `count: all`, every check counts, which is what a repository with no branch rules needs. Checks that don't count are reported and decide nothing. It polls at growing intervals, keeps within GitHub's rate limits, and the step must set a `timeout`. When all have finished it succeeds with `state: passed`, `failed` or `none`, the head commit, and each failed check's name, address, summary and the end of its log.
- **Each failed check says whether it also fails on the base branch.** A workflow can then tell a failure the pull request caused from one that was there already.
- **Merging is the connection's decision.** The merge handlers run only on a connection with `allow_merge: true`, false unless set. The default branch still can't be written to except by merging a pull request.
- **GitHub's auto-merge.** `github.enable_auto_merge` lets GitHub merge once every rule is met, including a required review, so Weir neither polls nor approves.
- **A handler that merges.** `github.merge_pull_request` merges with the method the step must name, only at the head commit it was given, and records the checks it saw in its output. It refuses, with `merged: false` and a reason, when a check failed or is still running, when no check ran, when the branch moved, or when GitHub's own rules refuse.
- **A commit can add to a branch that exists.** With `update: true`, `github.commit` puts a further commit on the branch when the step's files differ from it. Without `update` it behaves as before.
- **A handler can ask to be run again later.** A deterministic handler may answer "not yet, try again in this long". The step goes back in the queue and keeps its attempts, and the step's `timeout` is its limit, as for a poll step.
- **An expression can see how many times a step has run.** `steps.<id>.visit` is the number of that step's latest visit, so a `next` rule can stop a loop: `steps.checks.visit < 3`.

Out of scope:

- A general limit on visits to a step. The visit number is enough to write one in a rule.
- Re-running a failed check, or approving a pull request.
- Merging past a branch rule. The handler never uses an administrator's override.
- Reading checks or merging on anything but GitHub.
- Waking a waiting step when GitHub's webhook arrives. That is issue 156; polling is enough to work.
- Marking text from outside so that it stays marked inside a prompt. That is issue 157. Until it is built, a definition that maps a check's log into a prompt must treat it as untrusted itself, and the docs say so.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `github`: two new handlers, a commit that can add to a branch, and the permissions each new call asks for.
- `deterministic-steps`: a handler can ask to be run again later.
- `expressions`: a step's entry in `steps` holds its visit number.
- `definitions`: a `github.checks` step must set a `timeout`, and an unbounded loop is warned of.

## Impact

- `worker/`: `github.checks`, `github.merge_pull_request` and `github.enable_auto_merge`; `github.commit` reads `update`; the handler interface gains a "not yet" answer.
- `engine/`: a deferred deterministic step goes through the same path as a poll that is not done, in one transaction, and stops at the step's `timeout`. `scope()` adds `visit`.
- The database gains one column, `connections.allow_merge`, false unless set.
- The HTTP API gains two fields: `allow_merge` when a connection is put, and `warnings` in the reply to publishing a definition, present only when there is one.
- A GitHub App used with the new handlers needs three more permissions: `checks: read`, `statuses: read` and `actions: read`. Whether a check is required is read from GitHub's GraphQL API, per pull request, so the App needs no access to the repository's settings. An App without a permission gets an error that names it.
- Rules that must not be weakened:
  - **Weir stores references to secrets, never secrets.** Kept: both handlers use the connection, as `github.commit` does, and a token is minted for the one repository with the least each call needs.
  - **An agent is locked down.** Kept: no agent is given a new tool. The handlers are deterministic steps. A failed check's log is text from outside, and reaches an agent only if a definition maps it into a prompt. The docs say to treat it as untrusted; issue 157 enforces that.
  - **One transaction per transition.** Kept: deferring a handler commits the attempt, the step and its event together.
- `README.md`, `docs/build/02-epics.md`, and the specs and proof of the four capabilities.
