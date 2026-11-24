# Proof: command-line

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| Running weir with no command prints the usage | `cmd/weir/main_test.go:TestNoCommandPrintsUsage` |
| An unknown command is refused | `cmd/weir/main_test.go:TestUnknownCommand` |
| Help lists the commands and the settings | `cmd/weir/main_test.go:TestHelpListsEveryCommand` (`help` and ten command names are asserted; `-h`, `--help`, `key`, `version`, the settings, and `mcp` and `agent` refusing are not) |
| Version prints the version | `cmd/weir/main_test.go:TestVersion` |
| A command that fails says why and exits with 1 | `cmd/weir/main_test.go:TestMigrateNeedsADatabase`, `cmd/weir/main_test.go:TestCommandsCheckTheirArguments` |
| A command checks how many arguments it is given | `cmd/weir/main_test.go:TestCommandsCheckTheirArguments` (asserted for `publish`, `run`, `status`, `approve` and `connection`; not for `template`, `resume` or `key`) |
| Commands work on the database directly | Not tested: each command opens the database itself (`engine` in `cmd/weir/main.go`), and no test runs a command against a database |
| A command records who ran it | Not tested: the `--as` flag and its default are in `flags` in `cmd/weir/main.go`, and no test runs a command that records an actor |
| Migrate brings the database up to date | `engine/store/migrate_test.go:TestMigrateCreatesTheDefaultWorkspace`, `cmd/weir/main_test.go:TestMigrateNeedsADatabase` (the migration itself and the refusal without a database are asserted; the line the command prints is not) |
| Serve runs the API and a worker | `cmd/weir/main_test.go:TestWhetherAServerDoesTheWork` (how `WEIR_WORKER` is read is asserted; no test starts `serve`, so what it prints and that no worker starts are not) |
| Workflows, templates and connections are defined from the command line | Not tested: `publish`, `template` and `connection` in `cmd/weir/main.go` are only tested for their argument counts, never against a database |
| Runs are started and inspected from the command line | Not tested: `start`, `status` and `resume` in `cmd/weir/main.go` are only tested for their argument counts, never against a database |
| Tasks are listed and decided from the command line | Not tested: `tasks` and `approve` in `cmd/weir/main.go` are only tested for an argument count, never against a database |
| API keys are managed from the command line | Not tested: the `key` command in `cmd/weir/main.go` has no test; keys themselves are tested over the API in `api/auth_test.go` |
