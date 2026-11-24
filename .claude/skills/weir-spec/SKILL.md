---
name: weir-spec
description: Write the OpenSpec change for a piece of work in the Weir repository before any code — the proposal, the requirements it adds or changes, and the tasks — and check the specs. Use when starting a new feature or issue, when asked to "spec this", "write the spec", "propose a change" or "add a requirement", when the pre-commit hook or CI says a feature branch has no spec, and before committing anything that changes what Weir does.
---

# Writing the spec first

`openspec/specs/` says what Weir does today. A feature changes that, so it starts by saying how, in a form a person can agree to before the code exists. A commit on a `feature/` branch is refused until something under `openspec/` has changed.

Run OpenSpec as `./scripts/openspec`. It is the fixed version this repository is checked against, with usage statistics turned off. Wherever an OpenSpec skill says `openspec …`, run `./scripts/openspec …`.

## 1. Before you start

- You need the issue and its branch. If you have neither, use `weir-pick-work` first.
- Run `git config core.hooksPath`. If it does not say `.githooks`, run `make hooks`.
- Read the spec of each capability the work touches: `openspec/specs/<capability>/spec.md` and its `proof.md`. `./scripts/openspec list --specs` lists them.

## 2. Decide whether a spec is needed

| The work | What to do |
|---|---|
| Adds or changes something Weir does (a feature, a new field, an endpoint, a rule) | Write a change. Carry on |
| Fixes a bug so that Weir does what its spec already says | No change. Add the test, and name it in `proof.md` if the requirement had none |
| Fixes a bug where the spec itself describes the wrong behaviour | Write a change that modifies the requirement |
| Documents, refactoring, CI, dependencies | No change. The branch is `docs/` or `chore/`, not `feature/` |

## 3. Write the change

Name it after the issue, like the branch: `issue-0-credentials`.

Use the `openspec-propose` skill with that name. It makes `openspec/changes/issue-0-credentials/` and writes:

- **`proposal.md`:** why, what changes, which capabilities, and what is out of scope. Say which issue it closes.
- **`specs/<capability>/spec.md`:** the change to each capability, under `## ADDED Requirements`, `## MODIFIED Requirements` or `## REMOVED Requirements`. A modified requirement is written out in full, as it will read afterwards.
- **`tasks.md`:** the steps, as tick boxes. The last ones are always: the tests, `proof.md`, the to-do list and the docs, then `make check` and `make spec`.
- **`design.md`:** only when there is a decision worth recording.

Rules for a requirement here:

- Only what this change will build. Nothing that is planned for later.
- One behaviour, in under 450 characters, with SHALL or MUST. Put examples and edge cases in scenarios.
- At least one scenario, with **WHEN** and **THEN**.
- Behaviour someone can rely on: a status, an error type, a YAML key, a header. Not a function or a table.
- If it touches a rule in `CLAUDE.md` under "Rules that must not be weakened", the proposal says which and how the change keeps it.

## 4. Check it, and get it agreed

```sh
make spec
```

Then stop and show the person the proposal and the requirements. Do not write code until they agree. This is the cheap moment to change direction.

## 5. Build it

Work through `tasks.md` (the `openspec-apply-change` skill), ticking as you go. Write the test for each requirement as you build it.

## 6. Before the pull request

1. For every requirement added or changed, add or update its row in `openspec/specs/<capability>/proof.md`: `` `path/file_test.go:TestName` ``, or `Not tested:` and why. Name a test only if it asserts that behaviour.
2. Archive the change with the `openspec-archive-change` skill. That folds its requirements into `openspec/specs/` and moves the change to `openspec/changes/archive/`. It goes in the same pull request as the code.
3. `make spec` and `make check`.
4. Carry on with `weir-finish`.

## What stops a commit

The pre-commit hook (`.githooks/pre-commit`, turned on by `make hooks`) runs before every commit, and CI runs the same two checks:

- `./scripts/spec-gate`: on a `feature/` branch, something under `openspec/` must have changed.
- `make spec`: every spec is well formed, every requirement has a row in `proof.md`, and every test named exists.

If the hook stops you, fix what it names. Do not commit with `--no-verify`.
