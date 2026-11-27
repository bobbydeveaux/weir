## MODIFIED Requirements

### Requirement: The agent in a container is given every tool
Inside the container the agent SHALL run with every built-in tool and without a sandbox or permission prompts: the container is the boundary. Unless `WEIR_AGENT_PERMISSIONS` says otherwise, permission checks SHALL be off. It SHALL still load no user or project settings from the directory it works in, and no MCP servers but Weir's. It SHALL be given the container's environment, apart from variables that begin `WEIR_`.

#### Scenario: The command line in a container
- **WHEN** an agent is started in a container
- **THEN** its command line is not restricted to a list of tools and has no sandbox
- **AND** it is still told to ignore user and project settings and to use only the MCP configuration Weir gives it

#### Scenario: The setting is left out
- **WHEN** an agent is started in a container with no `WEIR_AGENT_PERMISSIONS`, or with `bypass`
- **THEN** its command line switches permission checks off, as before the setting existed

## ADDED Requirements

### Requirement: A setting keeps the agent's permission checks on
`WEIR_AGENT_PERMISSIONS`, read in the agent's container, SHALL choose how the agent is given its tools: `bypass` (the default) or `allow`. With `allow` the command line MUST NOT be told to bypass permission checks; it SHALL instead be told to refuse without asking anything not allowed, and SHALL be given every built-in tool and each of the step's tools as allowed, by name. Any other value SHALL stop the agent before it fetches its job, naming the setting.

#### Scenario: An organisation forbids bypassing permission checks
- **WHEN** an agent is started in a container with `WEIR_AGENT_PERMISSIONS=allow`
- **THEN** its command line does not bypass permission checks
- **AND** the shell, reading, editing, the web and skills are allowed by name, with no sandbox and no limit on which tools it has

#### Scenario: The step lists tools of its own
- **WHEN** such an agent's step lists a tool
- **THEN** that tool is allowed by name beside the built-in ones

#### Scenario: A value that is not known
- **WHEN** an agent is started with `WEIR_AGENT_PERMISSIONS=off`
- **THEN** it stops with an error that names the setting, and fetches no job
