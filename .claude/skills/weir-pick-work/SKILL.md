---
name: weir-pick-work
description: Choose one item from Weir's to-do list and set up to work on it without colliding with another session. Use when asked to "pick something up", "work on the next item", "take an epic", or to start a piece of roadmap work in this repository.
---

# Taking an item

1. Run `weir-status` first if you have not this session.
2. Choose one unticked item from `docs/build/02-epics.md`. Prefer, in order: something the person asked for; something that unblocks other items; something small enough to finish in one sitting.
3. Check nobody has it: `gh issue list --search "<a few words from the item>"` and `gh pr list`.
4. Claim it. If an issue exists, comment that you are starting. If not, open one titled with the item's own words, quoting the line from the list and naming its epic.
5. Branch from an up-to-date `main`: `git switch -c <epic-number>-<short-name>`.
6. Read the code you are about to change, and the section of `docs/spec/weir-spec.md` or `docs/build/` that covers it, before writing anything.

## Before you write code

Say, in a few lines, what you will build and how you will know it works. If the item touches one of the rules in `CLAUDE.md` under "Rules that must not be weakened", say which and how the change keeps it.

If the item is too big for one pull request, split it: add the smaller items to the list under the same epic, and take the first.

## When you are done

Use `weir-finish`.
