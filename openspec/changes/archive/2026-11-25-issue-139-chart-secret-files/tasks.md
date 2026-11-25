## 1. The Helm chart

- [x] 1.1 `secretFiles.volume`, `secretFiles.mountPath` and `secretFiles.adminKey` in `values.yaml`
- [x] 1.2 Mount the volume in the server and the migration, and set `WEIR_SECRETS_DIR` and `WEIR_ADMIN_KEY_FILE`
- [x] 1.3 Render the database's certificate authorities and the secret files as one list of mounts and one of volumes
- [x] 1.4 Refuse an admin key with no volume, an admin key with a slash, and no admin key at all
- [x] 1.5 Make `envFrom` depend on `existingSecret` only, as it does today, and check CI still renders the chart

## 2. Tests

- [x] 2.1 A CSI volume, a Secret volume, both beside `databaseCA`, and `migrate.when: start`
- [x] 2.2 Each refusal, and a server that does not require an admin key
- [x] 2.3 The `existingSecret` path unchanged

## 3. Finish

- [x] 3.1 The chart's README and `values.yaml`
- [x] 3.2 `docs/build/02-epics.md`
- [x] 3.3 `openspec/specs/deployment/proof.md`
- [x] 3.4 Archive the change, then `make check` and `make spec`
