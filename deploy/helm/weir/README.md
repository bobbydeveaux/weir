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

Use your cluster's own way of making those Secrets if it has one: External Secrets Operator, Sealed Secrets, a cloud's secret store driver.

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
| `existingSecret` | A Secret whose keys become the server's environment. It must hold `WEIR_ADMIN_KEY` and the database | none |
| `env` | Any other setting, by name | none |
| `replicaCount` | Copies of the server. More than one is safe | 1 |
| `migrate.enabled` | Run `weir migrate` before each install and upgrade | true |
| `agents.runner` | `kubernetes` (a Job each) or `inline` (beside the worker) | `kubernetes` |
| `agents.image.repository`, `agents.image.tag` | What an agent runs in. Build your own on top of it to add toolchains | `ghcr.io/bobbydeveaux/weir-agent` |
| `agents.secret` | A Secret whose keys become an agent's environment: the model's credential | none |
| `agents.namespace` | Where agents' Jobs are made | the release's own |
| `agents.serviceAccount` | The account an agent's pod runs as, for a cloud's workload identity | the namespace's default |
| `agents.cpu`, `agents.memory` | An agent pod's requests and limits | the cluster's |
| `serviceAccount.annotations` | For the server's own workload identity | none |
| `ingress.enabled`, `.host`, `.className`, `.annotations`, `.tlsSecret` | Reaching Weir from outside the cluster | off |
| `resources`, `nodeSelector`, `tolerations`, `podAnnotations` | The usual | none |

## What it does not do

- **It runs no database.** Point it at one you already have.
- **It limits no network.** What an agent's pod may reach is for a NetworkPolicy of yours.
- **It does not run the console.**

## How it was checked

Installed on a local cluster: the migration ran, the server came up and refused a request with no key, and a real agent step ran as a Job that Weir started with its own service account. The agent's pod ran as a non-root user and held no Kubernetes credential. Not run on a managed cluster.
