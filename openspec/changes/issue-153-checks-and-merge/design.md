## Decisions

### Waiting is "run me again later", not `retry`

The issue first suggested that the handler fail while checks run, and that the step's `retry` act as the wait. That makes a slow pipeline look like a failing step: it fills the attempts list with failures, and the number of retries becomes a guess at how long a pipeline takes.

A poll step already has the right behaviour: a poll that is not done goes back in the queue, keeps its attempts, and stops at the step's `timeout`. A deterministic handler gets the same thing. It returns "not yet" with a delay, and the engine treats it as it treats a poll that is not done.

A step that uses `github.checks` must set a `timeout`: publishing refuses one without, as it refuses a callback step without one.

### Woken by webhooks, polling as a fallback
Polling every 30 seconds per waiting step, across many runs, would run into GitHub's rate limits. When the connection receives GitHub webhooks, a `check_run`, `check_suite` or `status` event for the head commit runs the waiting handler again at once, through the existing "an event moves on the steps that were waiting for it" path. Polling stays as the fallback: it backs off up to ten times `every`, and waits for the reset when GitHub's rate-limit headers say the token is nearly out.

### Two handlers, not one

Checking and merging are separate so that a workflow can act between them: send a failure to an agent, or to a person. `github.merge_pull_request` still reads the checks itself and refuses unless they passed, so a definition that calls it alone cannot merge a failing pull request by mistake.

### Merging is the connection's decision
Weir's GitHub spec already says the default branch cannot be written to, and merging a pull request writes to it. So the handlers that merge run only on a connection that sets `allow_merge: true`, false unless set. Whoever manages the connection, usually the repository's owners, decides whether Weir may merge there. A definition's author can't grant it. `github.put_file` still refuses the default branch: a merged pull request is the only way in.

### GitHub's auto-merge, and explicit merging
`github.enable_auto_merge` hands the merge to GitHub. GitHub merges once every requirement of the base branch is met (required checks, required reviews, any other rule), so Weir doesn't poll and there's no race on the head commit. It suits repositories whose rules require a person's approval: auto-merge waits for it, and Weir never approves.

`github.merge_pull_request` remains for workflows that must act between the checks passing and the merge. It checks the required checks itself and merges at `head_sha`. A rule GitHub enforces, such as a required review, still refuses it, and it reports `merged: false` with GitHub's reason.

### The merge is pinned to a commit

`github.checks` returns `head_sha`. `github.merge_pull_request` takes it and passes it to GitHub, which refuses the merge if the branch has moved. What was checked is what is merged.

### "No checks ran" is not a pass

A repository with no pipeline reports no checks. `github.checks` says `state: none`, and `github.merge_pull_request` refuses unless the step sets `allow_no_checks: true`.

### Which checks count: the ones GitHub requires
The checks that matter are the ones the base branch's rules require (branch protection and rulesets), as GitHub would apply them to the merge, plus any the step names in `checks`. Counting every check would let one optional, flaky check block merging for ever. Counting `skipped` or `neutral` as passed would let a required check skipped by a path filter slip through. So:
- a required check passes only on `success`;
- checks that aren't required are reported under `other`, and decide nothing.

`mergeable_state` is passed through, so a workflow can see GitHub's own view.
### What a failed check carries

- `name`, `url`, `conclusion`, and `summary` (the check's own, when it has one).
- `log`: the last 16 KiB of the job's log, for a GitHub Actions job, when the App may read it. Otherwise it is left out.
- `fails_on_base`: `true` when the newest finished check of the same name on the base branch's head also failed, `false` when it passed, and left out when the base has no such check.

The whole output is capped at 256 KiB. Logs are cut from the front, because the end is where the failure is.

### `update: true` on `github.commit`

Without `update`, running the handler again for a branch that exists pushes nothing. That rule makes a retried step safe, and it stays the default.

With `update: true`, the handler clones the branch, applies the step's files, and commits and pushes only if they change something. A retried step is still safe: the second time the files change nothing. The push is never forced.

### Text from outside is marked, not just documented
A failed check's log goes straight back to an agent that will act on it, which is exactly where prompt injection lands. So handlers mark the fields that hold text from outside (`log`, `summary`), the mark follows the value through expressions, and a prompt receives it inside an `<untrusted>` block that the text can't close. A definition can't turn this off. The rules that must not be weakened are enforced, not left to a note.

### The method is required
Many repositories allow only squash merges. A default of `merge` that is often refused is worse than asking, so publishing refuses a merge step without a `method`.

### The visit number

`steps.<id>.visit` is the number of the latest visit to that step, starting at 1. It is enough to bound a loop in a `next` rule, and it adds no new field to a definition. Publishing warns of a loop that no rule bounds with a `visit` test, because then only the run's budget stops it.

## To confirm while building

- That a `next` rule may send a run back to an earlier step. The engine allows it and no test does it. A test of a three-visit loop is in the tasks.
- That the files of a step whose workspace was a checkout of the branch apply cleanly on top of that branch.
