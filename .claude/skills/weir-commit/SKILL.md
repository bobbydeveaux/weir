---
name: weir-commit
description: Name a branch, write a commit message, or open a pull request in the Weir repository so that each is tied to its GitHub issue. Use whenever creating a branch, committing, amending, or opening or retitling a pull request here, and when the "Naming" check fails in CI. Every commit and pull request must start with the issue number, such as "#0: Add credentials on a connection".
---

# Naming a change after its issue

Every change in this repository closes one issue, and says so in four places. CI checks all four and refuses a pull request that gets one wrong.

| Where | Form | Example |
|---|---|---|
| Branch | `<kind>/issue-<number>-<short-name>` | `feature/issue-0-credentials` |
| Every commit subject | `#<number>: <what changed>` | `#0: Add credentials on a connection` |
| Pull request title | the same form | `#0: Add credentials on a connection` |
| Pull request description | `Closes #<number>` on a line of its own | `Closes #0` |

`<kind>` is one of `feature`, `fix`, `docs` or `chore`. The short name is lower-case words joined by hyphens.

The examples here use issue number 0, which can never exist, on purpose. See "A closing keyword always acts" below.

## Before anything else: find the issue

- The number is in brackets at the end of the item's line in `docs/build/02-epics.md`.
- If the work has no issue, open one first (`gh issue create`, with its `epic:NN-…` label) and add the number to the line in the list. There is no change without an issue, however small.
- One issue per pull request. If the work covers two, split it.

## The branch

```sh
git switch main && git pull
git switch -c feature/issue-0-credentials
```

## Before the commit

Every commit is checked first by the pre-commit hook: a `feature/` branch must have changed something under `openspec/`, and `make spec` must pass. If `git config core.hooksPath` does not say `.githooks`, run `make hooks` so that it does. If the hook stops the commit, fix what it names (the `weir-spec` skill says how) and commit again. Never pass `--no-verify`.

## The commit

Write the message with `-m` or `-F`, never in an editor:

```sh
git commit -m "#0: Add credentials on a connection" -m "Why, and anything a reader needs to know."
```

**Why not an editor:** git treats a line that starts with `#` as a comment and deletes it when a message is edited by hand, so the subject would vanish. `-m` and `-F` keep it. The same applies to `git commit --amend`: pass `-m` again. If you must use an editor, run `git config core.commentChar ";"` in this repository first.

After the number, the subject is a short sentence in the imperative, starting with a capital: "Add", "Fix", "Remove". The body says why. Do not add attribution or session links.

## The pull request

```sh
gh pr create --title "#0: Add credentials on a connection" --body "$(cat <<'BODY'
## What changed, and why
…

Closes #0
BODY
)"
```

Fill in the rest of the template. Pull requests are squashed when merged, so the title becomes the commit on `main`: make it read well on its own.

## A closing keyword always acts

GitHub closes an issue when a merged pull request or commit says `closes`, `fixes` or `resolves` beside its number. It does this wherever the words appear: in a sentence, in a table, in a code sample, in a quotation. It cannot tell an example from an instruction.

- Write a closing keyword beside a real issue number only when merging should close that issue.
- To mention another issue, write "see #N", "part of #N" or "not in this change: #N".
- When you quote or explain the naming rule in a pull request, a commit or a comment, keep the number 0.

This is not hypothetical: the pull request that introduced this rule used a real number in its example, and closed that issue when it merged.

## When the Naming check fails

The check prints each thing that is wrong. To fix:

- **Title or description:** `gh pr edit <number> --title "#0: …"` or `--body`. The check runs again by itself.
- **The last commit:** `git commit --amend -m "#0: …"`, then `git push --force-with-lease`.
- **An older commit:** `git rebase -i main` and reword it, with `core.commentChar` set as above, then `git push --force-with-lease`.
- **The branch name:** `git branch -m feature/issue-0-credentials`, push it, and open a new pull request from it. A pull request's branch cannot be renamed in place.
