# Proof: deployment

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| A database must be named, as one address or in parts | `internal/config/config_test.go:TestFromEnvRequiresDatabase`, `internal/config/config_test.go:TestTheDatabaseCanBeGivenInParts`, `cmd/weir/main_test.go:TestMigrateNeedsADatabase` (the address that is built is asserted; connecting with it is not) |
| The address to listen on comes from the environment | `internal/config/config_test.go:TestFromEnvDefaults`, `internal/config/config_test.go:TestTheDatabaseCanBeGivenInParts` |
| An unknown runner is refused | `internal/config/config_test.go:TestFromEnvRunner`, `internal/config/config_test.go:TestFromEnvRejectsUnknownRunner` (`WEIR_RUNNER` is asserted; `serve` refusing `api` and a bad `WEIR_AGENT_RUNNER` are not, and are in `serve` in `cmd/weir/main.go`) |
| Lists and pairs are read from single settings | `cmd/weir/main_test.go:TestListsAndPairsAreReadFromSettings` |
| Migrating creates the schema and the default workspace | `engine/store/migrate_test.go:TestMigrateCreatesTheDefaultWorkspace` (migrating is asserted; a server refusing a database that was never migrated is not, and is in `New` in `engine/engine.go`) |
| Migrations started together take turns | `engine/store/migrate_test.go:TestMigrationsStartedTogetherTakeTurns` (the test says it would also pass without the lock on a quiet machine; the lock is in `engine/store/migrate.go`) |
| There is an image for the server and one for agents | Not tested: `.github/workflows/images.yml` builds both images on a pull request that touches `deploy/`, and nothing starts a container from either |
| Docker Compose starts Postgres, and Weir when asked | Not tested: `compose.yaml` is used by hand and by `make up`; CI starts its own Postgres and never runs the `weir` profile |
| The Helm chart installs the server on Kubernetes | Not tested: CI runs `helm lint` and `helm template` on the chart (`.github/workflows/ci.yml`), and no test installs it or reads what it renders |
| A server can answer the API without doing the work | `cmd/weir/main_test.go:TestWhetherAServerDoesTheWork` (how the setting is read is asserted; a server that starts no worker, and two servers sharing a database, are not) |
