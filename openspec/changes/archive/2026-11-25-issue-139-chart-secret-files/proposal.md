## Why

The Helm chart delivers secrets only as environment variables from one Kubernetes Secret, `existingSecret`. A cluster that keeps its secrets in a cloud's secret manager, with no External Secrets Operator, has to copy them by hand into a long-lived Secret. That copy is the one that drifts and leaks.

Closes issue 139.

## What Changes

- A new value, `secretFiles.volume`, is a volume source copied as written into the pod: a Secret, a CSI volume (the Secrets Store CSI driver, with AWS, Azure, Google Cloud or Vault), or a projected volume.
- The volume is mounted read-only at `secretFiles.mountPath` (default `/etc/weir/secrets`) in the server and in the migration, and `WEIR_SECRETS_DIR` is set to that path, for `file:` references.
- `secretFiles.adminKey` names the file in the volume that holds the admin key, and sets `WEIR_ADMIN_KEY_FILE` to it. `existingSecret` becomes optional.
- The chart refuses to render with `secretFiles.adminKey` and no `secretFiles.volume`; with neither `existingSecret` nor `secretFiles.adminKey` while the server requires an admin key; and with an `adminKey` that is not a plain file name.
- The volume mounts and volumes of the database's certificate authorities and of the secret files are rendered as one list each, so a pod never has two `volumeMounts:` or `volumes:` keys.

Out of scope: reading the database's address or password from a file; making the Secrets Store CSI driver's `SecretProviderClass`, which stays the user's own; installing the chart on a cluster with a CSI driver.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `deployment`: the chart's requirement no longer says `existingSecret` is the only way in. Two requirements are added: mounting secret files from a volume, and the settings the chart refuses with them.

## Impact

- **Chart:** `values.yaml`, `templates/_helpers.tpl`, `templates/deployment.yaml`, `templates/migrate.yaml`, and the chart's README.
- **Server:** none here. `WEIR_SECRETS_DIR` exists already. `WEIR_ADMIN_KEY_FILE` comes from issue 138, which must merge first: a server without it ignores the setting and refuses to start with no admin key.
- **Rules that must not be weakened:** "Weir stores references to secrets, never secrets" is kept. The chart still holds no secret. It names a volume, and the files in it are read by the server at run time.
- **A workspace cannot reach the admin key.** A `file:` reference reads `<mountPath>/<workspace>/<name>`, and neither a workspace nor a name may hold a dot or a slash. The admin key file must sit at the top of the volume, which no reference can reach, so the chart refuses an `adminKey` with a slash in it.
