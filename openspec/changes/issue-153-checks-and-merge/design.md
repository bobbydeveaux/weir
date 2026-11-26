## Decisions

### Waiting is "run me again later", not `retry`

The issue first suggested that the handler fail while checks run, and that the step's `retry` act as the wait. That makes a slow pipeline look like a failing step: it fills the attempts list with failures, and the number of retries becomes a guess at how long a pipeline takes.

A poll step already has the right behaviour: a poll that is not done goes back in the queue, keeps its attempts, and stops at the step's `timeout`. A deterministic handler gets the same thing. It returns "not yet" with a delay, and the engine treats it as it treats a poll that is not done.

A step that uses `github.checks` should set a `timeout`. Without one it waits for as long as the checks run, as a poll step does.

### Two handlers, not one

Checking and merging are separate so that a workflow can act between them: send a failure to an agent, or to a person. `github.merge_pull_request` still reads the checks itself and refuses unless they passed, so a definition that calls it alone cannot merge a failing pull request by mistake.

### The merge is pinned to a commit

`github.checks` returns `head_sha`. `github.merge_pull_request` takes it and passes it to GitHub, which refuses the merge if the branch has moved. What was checked is what is merged.

### "No checks ran" is not a pass

A repository with no pipeline reports no checks. `github.checks` says `state: none`, and `github.merge_pull_request` refuses unless the step sets `allow_no_checks: true`.

### Which checks count

Both check runs (GitHub Actions and Apps) and commit statuses on the head commit. A check that was skipped or is neutral counts as passed. A check that was cancelled, timed out or needs action counts as failed.

### What a failed check carries

- `name`, `url`, `conclusion`, and `summary` (the check's own, when it has one).
- `log`: the last 16 KiB of the job's log, for a GitHub Actions job, when the App may read it. Otherwise it is left out.
- `fails_on_base`: `true` when the newest finished check of the same name on the base branch's head also failed, `false` when it passed, and left out when the base has no such check.

The whole output is capped at 256 KiB. Logs are cut from the front, because the end is where the failure is.

### `update: true` on `github.commit`

Without `update`, running the handler again for a branch that exists pushes nothing. That rule makes a retried step safe, and it stays the default.

With `update: true`, the handler clones the branch, applies the step's files, and commits and pushes only if they change something. A retried step is still safe: the second time the files change nothing. The push is never forced.

### The visit number

`steps.<id>.visit` is the number of the latest visit to that step, starting at 1. It is enough to bound a loop in a `next` rule, and it adds no new field to a definition.

## To confirm while building

- That a `next` rule may send a run back to an earlier step. The engine allows it and no test does it. A test of a three-visit loop is in the tasks.
- That the files of a step whose workspace was a checkout of the branch apply cleanly on top of that branch.
