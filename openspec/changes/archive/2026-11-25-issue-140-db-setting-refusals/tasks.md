## 1. Settings

- [x] 1.1 Refuse `WEIR_DATABASE_SSLROOTCERT` with `WEIR_DATABASE_URL`, saying to put `sslrootcert=` in the address or give the database in parts
- [x] 1.2 With `WEIR_DATABASE_IAM=aws`, refuse a list of hosts in `WEIR_DATABASE_HOST` or of ports in `WEIR_DATABASE_PORT`

## 2. Tests

- [x] 2.1 Both refusals, each naming its setting, and that one host and port are still accepted

## 3. Finish

- [x] 3.1 `README.md`, the chart's README and values: `databaseCA` works only with the database in parts
- [x] 3.2 `docs/build/02-epics.md`
- [x] 3.3 `openspec/specs/deployment/proof.md`
- [x] 3.4 Archive the change, then `make check` and `make spec`
