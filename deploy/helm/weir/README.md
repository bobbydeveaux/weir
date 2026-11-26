# Weir's Helm chart

Installs the Weir server on a Kubernetes cluster: the API and a worker, a migration that runs before each install and upgrade, and, by default, leave for Weir to start each agent as a Job of its own.

```bash
kubectl create namespace weir

# The server's secrets. The chart holds none itself.
kubectl -n weir create secret generic weir \
  --from-literal=WEIR_ADMIN_KEY="$(openssl rand -hex 32)" \
  --from-literal=WEIR_DATABASE_URL='postgres://user:password@host:5432/weir'

# The model's credential, for agents. Weir never reads it.
kubectl -n weir create secret generic model-credential \
  --from-literal=CLAUDE_CODE_OAUTH_TOKEN=…

helm install weir deploy/helm/weir --namespace weir \
  --set existingSecret=weir --set agents.secret=model-credential
```

Use your cluster's own way of making those Secrets if it has one: External Secrets Operator or Sealed Secrets. To read secrets straight from a cloud's secret manager with no Secret in between, see "Secret files from a volume" below.

## What it makes

| Thing | Why |
|---|---|
| A Deployment | The server. A worker polls for work, so one copy stays up |
| A Service | So that agents' pods, and anything else in the cluster, can reach it |
| A Job, before each install and upgrade | `weir migrate`. A failed migration stops the release |
| A ServiceAccount | What the server runs as |
| A Role and RoleBinding | Leave to create, read and delete Jobs, and to read pods and their logs, in the namespace agents run in. Only with `agents.runner: kubernetes` |
| An Ingress | Only with `ingress.enabled: true` |

## Values

| Value | Meaning | Default |
|---|---|---|
| `image.repository`, `image.tag` | The server's image. The tag defaults to the chart's `appVersion` | `ghcr.io/bobbydeveaux/weir` |
| `existingSecret` | A Secret whose keys become the server's environment. It holds `WEIR_ADMIN_KEY` unless `secretFiles.adminKey` gives it, and the database unless that is signed in to with IAM | none |
| `secretFiles.volume` | A volume of secret files, copied as written into the pod: a Secret, a CSI volume or a projected volume. Mounted read-only, with `WEIR_SECRETS_DIR` set to it | none |
| `secretFiles.mountPath` | Where the volume is mounted | `/etc/weir/secrets` |
| `secretFiles.adminKey` | The file at the top of the volume that holds the admin key. Sets `WEIR_ADMIN_KEY_FILE` | none |
| `env` | Any other setting, by name | none |
| `databaseCA.configMap`, `databaseCA.key` | A ConfigMap of yours holding the certificate authorities for the database, and the file in it. The chart mounts it and sets `WEIR_DATABASE_SSLROOTCERT`. Only for a database given in parts: Weir refuses it beside `WEIR_DATABASE_URL`, which must carry `sslrootcert=` itself | none, `ca.pem` |
| `replicaCount` | Copies of the server. More than one is safe | 1 |
| `worker.concurrency` | How many steps each copy runs at once. Sets `WEIR_WORKER_CONCURRENCY`, from 1 to 256. Usually cheaper than more copies, since an ai step mostly waits on a model | 1 |
| `migrate.enabled` | Bring the database up to date with `weir migrate` | true |
| `migrate.when` | `before`: in a Job before each install and upgrade. `start`: in the server's pod before it serves, for a cluster that fills Secrets in after install (External Secrets Operator) | `before` |
| `agents.runner` | `kubernetes` (a Job each) or `inline` (beside the worker) | `kubernetes` |
| `agents.image.repository`, `agents.image.tag` | What an agent runs in. Build your own on top of it to add toolchains | `ghcr.io/bobbydeveaux/weir-agent` |
| `agents.secret` | A Secret whose keys become an agent's environment: the model's credential | none |
| `agents.namespace` | Where agents' Jobs are made | the release's own |
| `agents.serviceAccount` | The account an agent's pod runs as, for a cloud's workload identity | the namespace's default |
| `agents.cpu`, `agents.memory` | An agent pod's requests and limits | the cluster's |
| `serviceAccount.annotations` | For the server's own workload identity | none |
| `ingress.enabled`, `.host`, `.className`, `.annotations`, `.tlsSecret` | Reaching Weir from outside the cluster | off |
| `resources`, `nodeSelector`, `tolerations`, `podAnnotations` | The usual | none |

## Amazon RDS with IAM

A database that is signed in to with AWS IAM has no password, so the Secret holds only the admin key and the database's settings go in `env`.

```bash
# Amazon's certificate authorities for RDS, as a ConfigMap. They are public.
curl -sSfo global-bundle.pem https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem
kubectl -n weir create configmap weir-db-ca --from-file=ca.pem=global-bundle.pem
```

```yaml
# values.yaml
existingSecret: weir          # holds WEIR_ADMIN_KEY only
env:
  WEIR_DATABASE_IAM: aws
  WEIR_DATABASE_HOST: mydb.abc123.eu-west-2.rds.amazonaws.com
  WEIR_DATABASE_NAME: weir
  WEIR_DATABASE_USER: weir
databaseCA:
  configMap: weir-db-ca
migrate:
  when: start                 # the Job that runs before an install has no AWS identity
serviceAccount:
  annotations:                # for an IAM role for a service account; EKS Pod Identity needs none
    eks.amazonaws.com/role-arn: arn:aws:iam::111122223333:role/weir
```

- The server's IAM role needs `rds-db:connect` on the database user, and the user needs `GRANT rds_iam`.
- EKS sets `AWS_REGION` in the pod. Elsewhere, add it to `env`.
- The chart refuses `env.WEIR_DATABASE_IAM` with `migrate.when: before`. That Job runs before the chart's ServiceAccount exists, so it could not sign in.

This has been rendered and not installed: it has not met a real RDS database.

## Secret files from a volume

The server can read its secrets from files, mounted from any volume you describe: the admin key, and the files that connections name as `file:` references. A cluster with the [Secrets Store CSI driver](https://secrets-store-csi-driver.sigs.k8s.io/) can then read them from AWS Secrets Manager, Azure Key Vault, Google Secret Manager or Vault each time a pod starts, with no long-lived Secret to copy them into.

```yaml
# values.yaml
secretFiles:
  volume:
    csi:
      driver: secrets-store.csi.k8s.io
      readOnly: true
      volumeAttributes:
        secretProviderClass: weir   # yours: it says which secrets become which files
  adminKey: admin-key
```

The volume is mounted read-only at `/etc/weir/secrets` in the server and in the migration. The chart sets:

- `WEIR_SECRETS_DIR` to `/etc/weir/secrets`, so a `file:github-token` reference in the workspace `acme` reads `/etc/weir/secrets/acme/github-token`;
- `WEIR_ADMIN_KEY_FILE` to `/etc/weir/secrets/admin-key`.

A Secret, or a projected volume, works the same way:

```yaml
secretFiles:
  volume:
    secret:
      secretName: weir-files
      items:
        - { key: admin-key, path: admin-key }
        - { key: acme-github-token, path: acme/github-token }
  adminKey: admin-key
```

- **The admin key is a file at the top of the volume.** No `file:` reference can name a file there, because neither a workspace nor a name may hold a slash or a dot. The chart refuses an `adminKey` with a slash in it: a file in `acme/` could be read by the workspace `acme`.
- **Leave `WEIR_ADMIN_KEY` out of `existingSecret` when `secretFiles.adminKey` is set.** The server refuses to start with both.
- **`existingSecret` is optional** when the admin key is a file and the database needs no password from it, as with IAM sign-in (below). The database's address and password are not read from files, so a database signed in to with a password still needs `existingSecret` for `WEIR_DATABASE_URL` or `WEIR_DATABASE_PASSWORD`.
- **A CSI driver that signs in as the pod's service account** needs `migrate.when: start`. The Job that migrates before an install runs before the chart's ServiceAccount exists, so its volume cannot be mounted.
- The chart refuses to render with `secretFiles.adminKey` and no `secretFiles.volume`, and with neither `existingSecret` nor `secretFiles.adminKey`. A server whose `env.WEIR_REQUIRE_ADMIN_KEY` is set to an empty string does not require an admin key, and may have neither.

`WEIR_ADMIN_KEY_FILE` needs a server from issue #138 or later; an older one ignores it and will not start with no admin key. This has been rendered and not installed: it has not met a CSI driver.

## What it does not do

- **It runs no database.** Point it at one you already have.
- **It limits no network.** What an agent's pod may reach is for a NetworkPolicy of yours.
- **It does not run the console.**

## How it was checked

Installed on a local cluster: the migration ran, the server came up and refused a request with no key, and a real agent step ran as a Job that Weir started with its own service account. The agent's pod ran as a non-root user and held no Kubernetes credential. Not run on a managed cluster.
