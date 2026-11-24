# agent-tools Specification

## Purpose
An agent has no tools of its own. Every tool call goes through Weir's tool proxy, which checks it against the list its step declares and records it, so that what an agent can do is what its step says and nothing more.

## Requirements

### Requirement: An agent is offered only the tools its step lists
An ai step SHALL list the tools its agent may call under `tools.read` and `tools.write`. The agent SHALL be told about those tools and no others, and SHALL reach them only through Weir, presenting the one-time token of its own attempt.

#### Scenario: One tool is listed
- **WHEN** an agent whose step lists `orders.lookup` asks which tools it has, and the server also has `orders.cancel`
- **THEN** it is offered `orders.lookup` and nothing else

### Requirement: Every tool call is checked against the step's list
The tool proxy SHALL run a call only when the step of the attempt that holds the token lists that tool. A call to any other tool SHALL be refused and the tool SHALL NOT run. Over the API the refusal is 403, and the agent is told the call failed, so that it can carry on without the tool.

#### Scenario: A tool the step did not list
- **WHEN** an agent whose step lists only `orders.lookup` calls `orders.cancel`
- **THEN** the call is refused and the order is not cancelled
- **AND** a request made straight to the API for the same tool gets 403

### Requirement: Every tool call is recorded
The tool proxy SHALL record every call against the attempt that made it: the tool, whether it is a read or a write, the arguments, the result, any error and how long it took, with the status `succeeded`, `failed` or `denied`. A call that was refused SHALL be recorded as `denied`. The calls SHALL appear in the run's record. A result larger than 16 KB SHALL be recorded as its size and not its contents.

#### Scenario: Three calls, three records
- **WHEN** an agent makes one call that works, one whose tool fails, and one to a tool its step did not list
- **THEN** the run's record holds the three calls, as `succeeded`, `failed` and `denied`

### Requirement: A tool call needs the token of a running attempt
The token of an attempt SHALL be the only credential a tool call needs. Once the attempt has finished, its token SHALL open nothing: a call that presents it SHALL be refused with 409.

#### Scenario: The step is over
- **WHEN** an attempt completes and a tool call then presents its token
- **THEN** the reply is 409 and the tool does not run

### Requirement: A tool's own failure is passed back to the agent
When a tool runs and fails, the proxy SHALL record the call as `failed` and give the agent the tool's own message as a failed call. The step SHALL NOT fail because a tool did.

#### Scenario: The tool cannot find what was asked for
- **WHEN** an agent looks up an order that does not exist
- **THEN** the agent is told the call failed, with the message "no such order"

### Requirement: A tool is a write unless it says it only reads
Every tool SHALL have a class, read or write. A tool registered with no class SHALL be a write. A tool from an MCP server SHALL be a read only when the server marks it read-only, or when the whole connection was declared read-only.

#### Scenario: A server says nothing about a tool
- **WHEN** an MCP server offers `search` with no hint and `peek` marked read-only
- **THEN** `search` is a write and `peek` is a read

### Requirement: Publishing checks the tools a step lists
Publishing SHALL refuse a definition that lists a tool the workspace cannot reach, and one that lists a tool that can change things under `tools.read`. A read tool MAY be listed under `tools.write`.

#### Scenario: A write listed as a read
- **WHEN** a definition lists `orders.cancel`, which can change things, under `tools.read`
- **THEN** publishing is refused, and the error says it must be listed under write

#### Scenario: A tool nobody has
- **WHEN** a definition lists `orders.nonsense`
- **THEN** publishing is refused as not found

### Requirement: A workspace's MCP servers give tools to that workspace only
A connection of kind `mcp` SHALL ask the MCP server for its tools when the connection is made, and add each to the workspace as `<connection>.<tool>`. Only that workspace SHALL see the tools, list them in a definition, or call them, and every call SHALL still go through the tool proxy. A server that cannot be asked for its tools SHALL be refused with 502.

#### Scenario: Another workspace tries to use the tool
- **WHEN** the workspace `acme` connects an MCP server as `kb`, and the workspace `rival` publishes a definition that lists `kb.search`
- **THEN** publishing in `rival` is refused
- **AND** `rival` does not see `kb.search` in the registry

#### Scenario: The owning workspace calls it
- **WHEN** a running step in `acme` that lists `kb.search` calls it
- **THEN** the MCP server receives one call, and the call is recorded

### Requirement: The HTTP tools go through a named connection
The server SHALL offer `http.get`, a read, and `http.post`, a write. An agent names a connection of its run's workspace and a path. Weir SHALL make the request to that connection's address with the connection's credential, which the agent never sees. A path that does not start with a slash, or that names another address, and a connection the workspace does not have, SHALL each be refused.

#### Scenario: An agent fetches a path
- **WHEN** an agent calls `http.get` with the connection `orders` and the path `/orders/A-17`
- **THEN** the request goes to the `orders` connection's address at that path

#### Scenario: An agent names an address
- **WHEN** an agent calls `http.get` with the path `http://elsewhere.example/x`
- **THEN** the call fails and nothing is sent there

### Requirement: The GitHub tools read a repository and propose a change
The server SHALL offer three read tools, `github.get_file`, `github.list_files` and `github.get_pull_request`, and four write tools, `github.create_branch`, `github.put_file`, `github.open_pull_request` and `github.comment`. They work through a connection, which adds the token. A write that has already been made SHALL succeed again without changing anything.

#### Scenario: A step is tried again after it worked
- **WHEN** an agent creates a branch that exists, writes content that is already there, or opens a pull request that is already open
- **THEN** each call succeeds and changes nothing

### Requirement: A GitHub tool stays in its repository and off the default branch
A connection whose address names one repository SHALL work on that repository only. `github.put_file` SHALL refuse to write to the default branch.

#### Scenario: A write to the default branch
- **WHEN** an agent calls `github.put_file` for the branch `main`, which is the default branch
- **THEN** the call fails and nothing is sent to GitHub

#### Scenario: A connection for one repository
- **WHEN** an agent uses a connection for `acme/app` to read a file from `acme/payroll`
- **THEN** the call fails, saying the connection is for `acme/app`
