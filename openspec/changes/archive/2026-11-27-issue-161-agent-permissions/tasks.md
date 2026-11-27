## 1. Runner

- [x] 1.1 Add `Permissions` to `runner.Claude`, used only when `Unrestricted` is set: empty or `bypass` as today, `allow` to keep permission checks on and allow every tool by name
- [x] 1.2 Refuse any other value

## 2. Settings

- [x] 2.1 Read `WEIR_AGENT_PERMISSIONS` in `weir agent`, refusing an unknown value before the job is fetched

## 3. Tests

- [x] 3.1 The command line's arguments with `allow`: no bypass, nothing asked, built-in tools and the step's tools allowed by name
- [x] 3.2 The arguments are unchanged when the setting is left out
- [x] 3.3 An unknown value is refused, and no job is fetched

## 4. Finish

- [x] 4.1 `README.md` and `docs/build/03-agent-runners.md`
- [x] 4.2 `docs/build/02-epics.md`
- [x] 4.3 `proof.md` for `isolated-agents`
- [x] 4.4 Archive the change, then `make check` and `make spec`
