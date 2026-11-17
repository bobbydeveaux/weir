---
name: weir-status
description: Load where the Weir project stands and what is left to do. Use at the start of any session in this repository, before choosing or starting work, and whenever asked "where are we", "what's left", "what's next", or for the to-do list, roadmap, progress or epics.
---

# Where Weir stands

Do these in order, then report.

1. Read `docs/build/02-epics.md` in full. It has a status table by epic, the definition of "working", the decisions already made, and every item as a tick box.
2. Run `git log --oneline -15` and `git status --short` to see what changed recently and whether anything is uncommitted.
3. If GitHub is reachable, run `gh issue list --state open --limit 30` and `gh pr list` to see what other people or agents have claimed.
4. Check the tree is healthy: `make check` (needs Docker for Postgres; `make up` first). If it fails, that is the first thing to report.

## Report

Keep it short and lead with what matters:

- Whether the tree builds and the tests pass.
- What was finished most recently.
- What is in progress, and by whom, if issues or pull requests say.
- The three or four most useful items to do next, each with the epic it belongs to and what it unblocks.
- Anything in the list that looks wrong against the code.

Do not start work as part of this skill. To choose an item and begin, use `weir-pick-work`.

## Reading the list honestly

- A ticked item with a caveat in brackets is only as done as the caveat says.
- "Tested" means there is an automated test. "Run for real" means a live call was made. The list says which; keep that distinction when you report.
- The status table at the top can lag the tick boxes below it. If they disagree, the tick boxes and the code are right, and the table should be fixed.
