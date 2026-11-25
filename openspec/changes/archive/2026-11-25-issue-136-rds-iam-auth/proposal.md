## Why

Weir signs in to Postgres with a password. Some platforms give an application a role on Amazon RDS that can sign in only with AWS IAM: no password exists, so there is nothing to store or rotate. Weir cannot run against such a database today.

Closes issue 136.

## What Changes

- A new setting, `WEIR_DATABASE_IAM=aws`, makes Weir sign in with an RDS IAM token in place of a password. It is used with the database in parts: `WEIR_DATABASE_HOST`, `_PORT`, `_NAME` and `_USER`.
- A new token is made for every connection Weir opens, signed with the AWS credentials of the place it runs (EKS Pod Identity, IAM roles for service accounts, or any other source in AWS's default chain) and the region in `AWS_REGION`. A token is needed only to open a connection, so connections already in the pool go on working after it expires.
- `weir serve`, `weir migrate` and every other command that opens the database connect the same way.
- With IAM sign-in the connection is always made with `sslmode=verify-full`, because the token is sent as the password. Weir refuses to start with any other mode, with a password, with `WEIR_DATABASE_URL`, or with no region.
- A new setting, `WEIR_DATABASE_SSLROOTCERT`, names the file of certificate authorities that the database's certificate is checked against. RDS's certificates are signed by Amazon's own authorities, which a machine does not trust by default.
- The Helm chart can mount that file from a ConfigMap (`databaseCA.configMap`), and refuses `migrate.when: before` with IAM sign-in, because the Job that runs before an install has no AWS identity.

Out of scope: IAM sign-in for Google Cloud SQL and Azure; working out the region from the host name; a bundle of RDS's certificate authorities inside the image; RDS Proxy, which is expected to work and has not been tried.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `deployment`: four requirements are added: naming the certificate authorities for the database, signing in with an IAM token, the settings IAM sign-in refuses, and what the Helm chart does for it.

## Impact

- **Code:** `internal/config` reads and checks the settings. A new package, `internal/database`, opens the pool and the migration's connection, and makes the tokens. `engine/store` can migrate a connection it is handed. `cmd/weir` uses them.
- **Dependencies:** `github.com/aws/aws-sdk-go-v2/feature/rds/auth` and `github.com/aws/aws-sdk-go-v2/config` become direct. The AWS SDK was already in the build, brought in by the file store. `rds/auth` signs a token locally and calls no AWS service. Writing the signing by hand would mean keeping a copy of AWS's signature scheme.
- **Rules that must not be weakened:** "Weir stores references to secrets, never secrets" is kept. A token lives in memory for the moment a connection is opened, and is never written to the database, a log or an error.
- **Not proven here:** a sign-in to a real RDS database. The tests prove the token is well formed, and that a password made for each connection is what Postgres is given.
