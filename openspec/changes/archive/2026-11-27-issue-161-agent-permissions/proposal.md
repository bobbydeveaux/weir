## Why

An agent in a container of its own is started with the command line's permission checks switched off (`--dangerously-skip-permissions`). Some organisations forbid that mode: a managed setting disables it for every sign-in the organisation owns. An agent that signs in under such a policy still starts, but every shell command it tries is refused, because nobody is there to approve it. The step ends with the agent reporting that it could do nothing.

Closes issue 161.

## What Changes

- A new setting, `WEIR_AGENT_PERMISSIONS`, read by `weir agent` inside the agent's container:
  - `bypass`, the default, is what happens today: permission checks are off.
  - `allow` keeps permission checks on. Every built-in tool and each of the step's own tools is allowed by name, and anything not allowed is refused without waiting for a person.
- Any other value stops the agent before it fetches its job, naming the setting.
- Everything else about the agent is the same either way: no sandbox, no project settings, only Weir's MCP servers, the same environment.

Out of scope: an agent that runs beside the worker, which never bypasses permission checks; choosing which tools an agent in a container may use; carrying the setting from the server to the container (it is set where the container's environment is set: the image, the Kubernetes Secret, the Cloud Run job, or `WEIR_AGENT_ENV` with Docker).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `isolated-agents`: the agent in a container is given every tool either by switching permission checks off or, when asked, by allowing each tool by name.

## Impact

- **Code:** `runner/claude.go` (the arguments for an agent in a container), `cmd/weir` (reading the setting in `weir agent`).
- **Dependencies:** none.
- **Rules that must not be weakened:** none is touched. The agent beside the worker is unchanged, and still never bypasses permission checks. `allow` gives the agent in a container no more than `bypass` does; it is the narrower of the two, because a deny rule in a managed policy still applies.
- **Who is affected:** nobody, unless they set the new value.
