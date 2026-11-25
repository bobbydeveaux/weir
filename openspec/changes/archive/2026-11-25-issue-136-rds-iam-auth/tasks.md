## 1. Settings

- [x] 1.1 Read `WEIR_DATABASE_IAM`, and refuse any value other than `aws`
- [x] 1.2 With IAM sign-in, refuse `WEIR_DATABASE_URL`, a password, a missing host or user, a socket, a missing region, and any `sslmode` other than `verify-full`; default the mode to `verify-full`
- [x] 1.3 Read `WEIR_DATABASE_SSLROOTCERT` into the address built from parts

## 2. Connecting

- [x] 2.1 Add `internal/database`: open the pool and the migration's connection, with a token made before each connection
- [x] 2.2 Make an RDS IAM token from AWS's default credentials, for the host, port and user of the address
- [x] 2.3 Let `engine/store` migrate a connection it is handed
- [x] 2.4 Use them in `cmd/weir` for every command, and say so in `weir help`

## 3. The Helm chart

- [x] 3.1 `databaseCA.configMap` and `databaseCA.key`: mount the ConfigMap and set `WEIR_DATABASE_SSLROOTCERT`
- [x] 3.2 Refuse `env.WEIR_DATABASE_IAM` with `migrate.when: before`
- [x] 3.3 Check both in CI with `helm template`

## 4. Tests

- [x] 4.1 The settings: each refusal, the default mode, the root certificates
- [x] 4.2 A token: its host, port, user, region, service and lifetime
- [x] 4.3 Against Postgres: a password made for each connection, a changed password, a failed token, and a migration

## 5. Finish

- [x] 5.1 `README.md`, `docs/deploy/your-own-platform.md`, the chart's README and values
- [x] 5.2 `docs/build/02-epics.md`
- [x] 5.3 `openspec/specs/deployment/proof.md`
- [x] 5.4 Archive the change, then `make check` and `make spec`
