# Running Weir on your own platform

Weir is published as two container images. Whatever you deploy with, the shape is the same: a small repository of your own that starts from Weir's image at a version you choose, plus your platform's own configuration. None of Weir's code is in it, and none of your platform's configuration is in Weir.

| Image | What it is |
|---|---|
| `ghcr.io/bobbydeveaux/weir` | The server: the API and a worker in one program |
| `ghcr.io/bobbydeveaux/weir-agent` | What an agent runs in, when agents have containers of their own |

Tags: `edge` is the latest commit on `main`; a version such as `0.1.0` is a release. Pin one.

## What the server needs

| Need | How |
|---|---|
| A Postgres database | `WEIR_DATABASE_URL`, or `WEIR_DATABASE_HOST` with `_PORT`, `_NAME`, `_USER`, `_PASSWORD` and `_SSLMODE` for a platform that hands out the parts. The host may be a socket directory, as Cloud SQL's is. For Amazon RDS with no password, `WEIR_DATABASE_IAM=aws`: see [the README](../../README.md#a-database-with-no-password-amazon-rds-and-iam) |
| Its schema | Run `weir migrate` once per deploy, before the new version takes traffic |
| An admin key | `WEIR_ADMIN_KEY`, from your platform's secret store, or `WEIR_ADMIN_KEY_FILE` naming a file that holds it, where the platform hands out secrets as files. The image refuses to start without one |
| A port | It listens on `PORT` if your platform sets that, otherwise 8080 |
| Secrets for connections | `WEIR_SECRET__<WORKSPACE>__<NAME>` variables, or files under `WEIR_SECRETS_DIR` |
| To stay running | A worker polls for work, so one instance must stay up. A platform that stops idle services needs to be told to keep one |

## On Kubernetes

Use the chart, [deploy/helm/weir](../../deploy/helm/weir/README.md). Your own repository then holds a values file and whatever makes your Secrets, and names the chart.

## A repository of your own

```dockerfile
# Dockerfile
FROM ghcr.io/bobbydeveaux/weir:edge
COPY --chmod=755 start.sh /usr/local/bin/start.sh
ENTRYPOINT ["start.sh"]
CMD ["serve"]
```

```sh
#!/bin/sh
# start.sh: turn the names your platform uses into the ones Weir reads.
# This example is for a platform that sets DB_HOST, DB_USER and so on.
export WEIR_DATABASE_HOST="$DB_HOST" WEIR_DATABASE_PORT="$DB_PORT" WEIR_DATABASE_NAME="$DB_NAME"
export WEIR_DATABASE_USER="$DB_USER" WEIR_DATABASE_PASSWORD="$DB_PASSWORD"
exec weir "$@"
```

Beside those two files goes whatever your platform reads: a Helm values file, a Cloud Run service definition, a Terraform module, or a platform's own one-file description of an app. Its migration step is `weir migrate` (or `start.sh migrate`, so that it sees the same settings).

## Not built yet

- **Agents on AWS.** The server can start an agent's container with Docker (a laptop), as a Kubernetes Job (a cluster), or as a run of a Cloud Run job. It cannot yet start a Fargate task.
- **Scale to zero.** Timers, retries and waiting work are found by polling.
- **The console** is not in the server image.
