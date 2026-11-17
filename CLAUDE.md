# Working on Weir

Weir is a workflow engine built for AI: a stateless Go service on Postgres, with a SvelteKit console in `console/`.
These rules apply to every person and every agent working in this repository.

## Start here

- **Where things stand, and what to do next:** `docs/build/02-epics.md`. It is the project's single to-do list.
  Run the `weir-status` skill to load it, or read it before you change anything.
- **What Weir is meant to be:** `docs/spec/weir-spec.md`.
- **Why things are the way they are:** the other files in `docs/build/`.

## Commands

```sh
make up          # Postgres in Docker, on port 54329
make check       # what CI runs: formatting, vet, and every test against Postgres
make build       # ./bin/weir
cd console && npm install && npm run check && npm run build
```

Tests need Postgres. `make test-db` sets `WEIR_TEST_DATABASE_URL`; without it the tests that need a database are skipped, and a skipped test proves nothing.

## What "done" means

A change is finished when all of these are true. The `weir-finish` skill walks through them.

1. `make check` passes, and `npm run check` passes if `console/` changed.
2. There is a test that fails without the change. Tests use a real Postgres, never a fake one.
3. `docs/build/02-epics.md` is updated in the same change: tick what is now done, and say plainly what is not.
4. Anything a user can see is documented: `README.md` for settings and commands, and the relevant file under `docs/`.
5. The summary of the change says what was tested and what was not.

## Rules that must not be weakened

These are the reasons Weir exists. A change that relaxes one needs a maintainer's agreement first, in an issue.

- **An agent is locked down.** It gets no built-in tools, no settings files, and almost none of the server's environment.
  Never run a model's command line in an auto-approve or bypass-permissions mode.
- **Every tool call goes through the tool proxy,** which checks it against the step's list and records it. Nothing gives an agent a tool directly.
- **A workspace cannot see or reach another workspace's anything.** Every query that reads a tenant's data is scoped by workspace.
  A new endpoint needs a test that a second workspace gets a 404 or an empty answer.
- **One transaction per transition.** A step's state, its event, and any hook or notification commit together or not at all.
- **Published versions are immutable,** and the event log is append-only. The database enforces both; do not work around the triggers.
- **A budget is a limit, not a report.** A step that would exceed it is stopped.
- **Weir stores references to secrets, never secrets.** An API key is stored only as a hash.
- **A provider that cannot give a guarantee refuses the step.** It never runs the step without the guarantee.

## How to write here

- **Code:** standard Go, `gofmt`. Comments say why, not what. Errors are sentences a person can act on.
- **Docs and user-facing text:** plain British English, short sentences, active voice. Say what is built and what is not, without softening.
  Never describe something that is designed as if it were built.
- **Dependencies:** none without a reason given in the change.
- **Migrations:** add a new numbered file under `engine/store/migrations/`. Never edit one that has been released.
- **Do not commit** secrets, keys, tokens, a person's email address, or paths from your own machine.

## Working alongside other agents

- Do one item from the to-do list at a time, on a branch, and open a pull request. Do not commit to `main`.
- Say which item you are taking: open or comment on a GitHub issue for it before you start, so that two sessions do not do the same work.
- If you find the to-do list is wrong (something marked done is not, or the reverse), fix the list in your change and say so.
- If you cannot finish, leave the list accurate: note what is half-done and where you stopped.
