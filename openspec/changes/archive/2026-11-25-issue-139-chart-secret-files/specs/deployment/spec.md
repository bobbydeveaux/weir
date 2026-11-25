## MODIFIED Requirements

### Requirement: The Helm chart installs the server on Kubernetes
The chart in `deploy/helm/weir` SHALL make a Deployment of the server, a Service and a ServiceAccount, and SHALL run `weir migrate` for each release. It holds no secret: `existingSecret`, when set, names a Secret whose keys become the server's environment, and `secretFiles` names a volume of secret files. The chart runs no database.

#### Scenario: A release is upgraded
- **WHEN** `helm upgrade` is run with the default values, where `migrate.when` is `before`
- **THEN** a Job runs `weir migrate` before each install and upgrade, before the new server starts
- **AND** a migration that fails stops the upgrade

#### Scenario: Migrating when the server starts
- **WHEN** the chart is installed with `migrate.when: start`
- **THEN** `weir migrate` runs in the server's pod before the server starts

#### Scenario: Agents run as Jobs
- **WHEN** the chart is installed with `agents.runner: kubernetes`
- **THEN** the server is set to start each agent as a Job
- **AND** the server's account may create, read and delete Jobs, and read pods and their logs, in the namespace agents run in, and nothing else

#### Scenario: Agents run beside the worker
- **WHEN** the chart is installed with `agents.runner: inline`
- **THEN** no Role or RoleBinding is made

#### Scenario: Health checks and the way in
- **WHEN** the chart is installed
- **THEN** the server's pod is checked on `/healthz`
- **AND** an Ingress is made only with `ingress.enabled`

## ADDED Requirements

### Requirement: The Helm chart mounts secret files from a volume
When `secretFiles.volume` is set, the chart SHALL copy it as written into the server's pod and the migration's, mount it read-only at `secretFiles.mountPath` (default `/etc/weir/secrets`) in every container that runs Weir, and set `WEIR_SECRETS_DIR` to that path. When `secretFiles.adminKey` is set, the chart SHALL set `WEIR_ADMIN_KEY_FILE` to that file in the mount. A pod SHALL have one list of volumes and each container one list of mounts.

#### Scenario: A CSI volume
- **WHEN** the chart is rendered with `secretFiles.volume.csi.driver: secrets-store.csi.k8s.io`, a `volumeAttributes.secretProviderClass`, and `secretFiles.adminKey: admin-key`
- **THEN** the server's pod and the migration's pod each have that CSI volume, mounted read-only at `/etc/weir/secrets`
- **AND** `WEIR_SECRETS_DIR` is `/etc/weir/secrets` and `WEIR_ADMIN_KEY_FILE` is `/etc/weir/secrets/admin-key`
- **AND** no `envFrom` is rendered

#### Scenario: A Secret volume, beside the database's certificate authorities
- **WHEN** the chart is rendered with `secretFiles.volume.secret.secretName: weir-files` and `databaseCA.configMap: weir-db-ca`
- **THEN** each pod has one `volumes` list holding both, and each container one `volumeMounts` list holding both

#### Scenario: No secret files
- **WHEN** the chart is rendered with `existingSecret` and no `secretFiles.volume`
- **THEN** nothing is mounted for secret files, and neither `WEIR_SECRETS_DIR` nor `WEIR_ADMIN_KEY_FILE` is set

### Requirement: The Helm chart refuses secret files that cannot work
The chart MUST refuse to render when `secretFiles.adminKey` is set with no `secretFiles.volume`; when `secretFiles.adminKey` holds a slash, because a file below the top of the volume is one a workspace's `file:` reference could read; and when neither `existingSecret` nor `secretFiles.adminKey` is set while the server requires an admin key, which it does unless `env.WEIR_REQUIRE_ADMIN_KEY` is set and empty. Each error SHALL name the value to set.

#### Scenario: An admin key with no volume
- **WHEN** the chart is rendered with `secretFiles.adminKey: admin-key` and no `secretFiles.volume`
- **THEN** rendering fails, and the error names `secretFiles.volume`

#### Scenario: No admin key at all
- **WHEN** the chart is rendered with no `existingSecret` and no `secretFiles.adminKey`
- **THEN** rendering fails, and the error names `existingSecret` and `secretFiles.adminKey`

#### Scenario: The admin key in a workspace's directory
- **WHEN** the chart is rendered with `secretFiles.adminKey: acme/admin-key`
- **THEN** rendering fails, and the error says the file must be at the top of the volume

#### Scenario: A server that does not require an admin key
- **WHEN** the chart is rendered with no `existingSecret`, no `secretFiles.adminKey`, and `env.WEIR_REQUIRE_ADMIN_KEY` set to an empty string
- **THEN** it renders
