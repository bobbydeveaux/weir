# For any coding agent

The rules for working in this repository are in [CLAUDE.md](CLAUDE.md). They apply whichever agent or model you are.

In short: read `docs/build/02-epics.md` before you start, run `make check` before you finish, and update that file and the docs in the same change.

What Weir does today is specified in `openspec/specs/`. A change in behaviour starts with an OpenSpec change under `openspec/changes/`, and `make spec` must pass. The OpenSpec skills are in `.agents/skills/`.
