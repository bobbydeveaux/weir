---
name: weir-finish
description: Finish a piece of work in the Weir repository properly — tests, formatting, the to-do list, the docs, and an honest summary. Use before committing, before opening a pull request, when asked to "wrap up", "finish", "commit this" or "open a PR", and whenever code in this repository has changed and the docs or the to-do list have not.
---

# Finishing work

A change is not finished until the to-do list and the docs say what the code now does. Go through every step; do not skip one because the change is small.

## 1. Prove it

- `make check` must pass. It runs formatting, `go vet`, and every test against Postgres.
- If `console/` changed: `cd console && npm run check && npm run build`.
- There must be a test that fails without your change. If you added an endpoint that reads a workspace's data, there must be a test that another workspace cannot read it.
- If the change calls a real model or a real third party, say whether you made a live call. A test against a stand-in server proves the request matches the documentation, not that the service accepts it.

## 2. Update the to-do list

Open `docs/build/02-epics.md`.

- Tick the item, and add in brackets anything that is not done or not proven ("tested against a stand-in; not run against a real account").
- If the work exposed something missing, add it as a new unticked item under the right epic.
- If an epic's state changed, update its row in the status table at the top.

## 3. Update the docs

| If you changed | Update |
|---|---|
| A setting, a command, or how to run it | `README.md` |
| The API (an endpoint, a header, a status code, a payload) | `README.md`, and say in the pull request that the website's docs page needs the same change |
| What a workflow can say (the YAML) | `docs/spec/weir-spec.md` and `examples/` |
| The database | `docs/build/01-database-design.md` |
| How models are reached | `docs/build/03-agent-runners.md` |
| A design decision | The decisions table in `docs/build/02-epics.md` |

Write what is true now. Remove any sentence your change has made false, including "not built yet" lines.

## 4. Check what you are about to commit

- `git status --short` and `git diff --stat`: only the files you meant to change.
- No secrets, keys, tokens, email addresses or paths from your own machine. No built files (`bin/`, `console/build/`, `node_modules/`).

## 5. Commit and open a pull request

- One idea per pull request, on a branch, never straight to `main`.
- The message says what changed and why, in plain sentences.
- Fill in the pull request template: what changed, how it was tested, what was not tested, and which to-do item it closes.

## 6. Say what happened

End with a short, honest summary: what works, what you verified and how, what you did not verify, and what should come next.
