# Weir

A workflow engine built for AI. Every step is one of five kinds (decide, AI, deterministic, human or external), and all five obey one contract: typed inputs and outputs, an owner, a budget and a full audit record. Weir runs as a stateless Go service on Postgres.

**Status: early build, not yet usable.** The repository has its foundations and nothing else. There is no engine to run a workflow yet. The first usable version will be tagged `v0.1.0`.

Site: [weir.stackramp.io](https://weir.stackramp.io)

## What works today

- `weir migrate` brings a Postgres database up to the current schema, which so far is one table.
- `weir version` prints the version.

## Develop

You need Go 1.26 or newer and Docker.

```sh
make up        # start Postgres on localhost:54329
make migrate   # apply the schema
make test      # tests that need no database
make test-db   # every test, against Postgres
make check     # what CI runs
```

Run `make help` for the full list.

## Configuration

| Variable | Meaning | Default |
| --- | --- | --- |
| `WEIR_DATABASE_URL` | Postgres connection string | required |
| `WEIR_HTTP_ADDR` | Address the API listens on | `:8080` |
| `WEIR_RUNNER` | How `ai` and `decide` steps run: `cli`, `api` or `fake` | `cli` |

The `cli` runner uses the Claude Code command line on a long-lived OAuth token. The `api` runner calls a model provider with an API key. The `fake` runner returns scripted answers and is used by the tests. Only the setting exists so far; the runners themselves are not built.

## Layout

| Path | Holds |
| --- | --- |
| `cmd/weir` | The command line |
| `engine/` | The engine. `engine/store` owns the Postgres schema |
| `internal/config` | Settings read from the environment |
| `spec/` | The workflow contract (empty so far) |
| `conformance/` | Tests any implementation must pass (empty so far) |
| `adapters/`, `handlers/`, `examples/` | Empty so far |

## Licence

Apache 2.0. See [LICENSE](LICENSE).
