# Working on Weir

Weir is a workflow engine built for AI: a stateless Go service on Postgres, with a SvelteKit console in `console/`.
These rules apply to every person and every agent working in this repository.

## Start here

- **Where things stand, and what to do next:** `docs/build/02-epics.md`. It is the project's single to-do list.
  Run the `weir-status` skill to load it, or read it before you change anything.
- **What Weir does today, requirement by requirement:** `openspec/specs/`. See "Specs" below.
- **What Weir is meant to be:** `docs/spec/weir-spec.md`.
- **Why things are the way they are:** the other files in `docs/build/`.

## Commands

```sh
make up          # Postgres in Docker, on port 54329
make check       # what CI runs: formatting, vet, and every test against Postgres
make build       # ./bin/weir
make spec        # the specs: their form, and that each requirement names what proves it
make hooks       # once in each clone: run the spec checks before every commit
cd console && npm install && npm run check && npm run build
```

Tests need Postgres. `make test-db` sets `WEIR_TEST_DATABASE_URL`; without it the tests that need a database are skipped, and a skipped test proves nothing.

## What "done" means

A change is finished when all of these are true. The `weir-finish` skill walks through them.

1. `make check` passes, and `npm run check` passes if `console/` changed.
2. There is a test that fails without the change. Tests use a real Postgres, never a fake one.
3. `docs/build/02-epics.md` is updated in the same change: tick what is now done, and say plainly what is not.
4. Anything a user can see is documented: `README.md` for settings and commands, and the relevant file under `docs/`.
5. If behaviour changed, the spec changed with it, and `make spec` passes. See "Specs" below.
6. The summary of the change says what was tested and what was not.

## Specs

`openspec/specs/` says what Weir does today: one folder for each capability, with a `spec.md` of requirements and scenarios, and a `proof.md` that names the test proving each requirement. It is kept with [OpenSpec](https://github.com/Fission-AI/OpenSpec).

- **A spec holds only what is built.** What is designed and not built stays in `docs/spec/weir-spec.md` and the to-do list.
- **A change in behaviour starts with an OpenSpec change.** Before writing code, write `openspec/changes/issue-0-short-name/`: a proposal, the requirements it adds, changes or removes, and the tasks. Agree it, then build it. The `weir-spec` skill walks through it.
- **A `feature/` branch must change something under `openspec/`.** The pre-commit hook and CI both refuse one that does not (`scripts/spec-gate`). Work that changes no behaviour is not a feature: name its branch `docs/`, `chore/` or `fix/`.
- **The change is archived in the same pull request** (`openspec-archive-change`), which folds its requirements into `openspec/specs/`. Pull requests are squashed, so `main` never holds a spec that is ahead of the code or behind it.
- **Every requirement has a row in `proof.md`:** the test that proves it, as `` `path/file_test.go:TestName` ``, or `Not tested:` and why. Name a test only if it asserts that behaviour. An honest "Not tested" is better than a test that only passes nearby.
- **A change with no effect on behaviour needs no OpenSpec change:** documents, refactoring, CI.
- **Run `make hooks` once in each clone.** Every commit then runs the two checks first, and is stopped if either fails. Never commit with `--no-verify`.
- **OpenSpec is run as `./scripts/openspec`,** the fixed version this repository is checked against, with its usage statistics turned off.
- **`make spec` checks it all on your own machine.** It needs Node 22.20 or later, and fetches a fixed version of OpenSpec the first time. The half that checks `proof.md` is an ordinary Go test, so `make check` runs it too.

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

## Naming

Every change closes one issue, and says so in four places. CI checks them (the `Naming` job), and the `weir-commit` skill has the detail.

| Where | Form |
|---|---|
| Branch | `feature/issue-0-credentials` (or `fix/`, `docs/`, `chore/`) |
| Every commit subject | `#0: Add credentials on a connection` |
| Pull request title | `#0: Add credentials on a connection` |
| Pull request description | `Closes #0` |

Write commit messages with `-m` or `-F`, not in an editor: git deletes a line that starts with `#` when a message is edited by hand.

The examples use issue number 0, which cannot exist. GitHub closes an issue whenever a merged pull request or commit says `closes`, `fixes` or `resolves` beside its number, even in an example or a quotation. Write one of those words beside a real number only when you mean it.

## Working alongside other agents

- Do one item from the to-do list at a time, on a branch named after its issue, and open a pull request. `main` accepts nothing else.
- Say which item you are taking before you start, so that two sessions do not do the same work. Every open item in the to-do list ends with its GitHub issue number: comment there. Labels (`epic:NN-…`) group issues by epic.
- If you add an item to the list, open an issue for it and put the number on the line.
- If you find the to-do list is wrong (something marked done is not, or the reverse), fix the list in your change and say so.
- If you cannot finish, leave the list accurate: note what is half-done and where you stopped.
