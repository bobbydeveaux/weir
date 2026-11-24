---
name: weir-pick-work
description: Choose one item from Weir's to-do list and set up to work on it without colliding with another session. Use when asked to "pick something up", "work on the next item", "take an epic", or to start a piece of roadmap work in this repository.
---

# Taking an item

1. Run `weir-status` first if you have not this session.
2. Choose one unticked item from `docs/build/02-epics.md`. Prefer, in order: something the person asked for; something that unblocks other items; something small enough to finish in one sitting.
3. Check nobody has it: `gh issue list --search "<a few words from the item>"` and `gh pr list`.
4. Claim it. Each open item ends with its issue number in brackets: comment on that issue that you are starting. If an item has no number, open an issue titled with the item's own words, label it with its epic (`epic:NN-…`), and add the number to the line.
5. Run `make hooks` if `git config core.hooksPath` does not say `.githooks`, so that the spec checks run before every commit.
6. Branch from an up-to-date `main`, named after the issue: `git switch -c feature/issue-0-credentials` (or `fix/`, `docs/`, `chore/`). The `weir-commit` skill has the naming rules.
7. Read the code you are about to change, the capability's spec in `openspec/specs/`, and the section of `docs/spec/weir-spec.md` or `docs/build/` that covers it, before writing anything.

## Before you write code

If the item changes what Weir does, use the `weir-spec` skill now: it writes the OpenSpec change (a proposal, the requirements it adds or changes, and the tasks) and gets it agreed before any code. That is where you say what you will build. A commit on a `feature/` branch is refused until the spec is there. A change that alters no behaviour needs none.

Otherwise say, in a few lines, what you will build and how you will know it works. If the item touches one of the rules in `CLAUDE.md` under "Rules that must not be weakened", say which and how the change keeps it.

If the item is too big for one pull request, split it: add the smaller items to the list under the same epic, and take the first.

## When you are done

Use `weir-finish`.
