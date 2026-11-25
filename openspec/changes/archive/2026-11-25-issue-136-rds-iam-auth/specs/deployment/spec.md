## ADDED Requirements

### Requirement: The certificate authorities for the database can be named
When the database is given in parts, `WEIR_DATABASE_SSLROOTCERT` SHALL name a file of certificate authorities, and Weir SHALL check the database's certificate against that file.

#### Scenario: A file of certificate authorities is named
- **WHEN** `WEIR_DATABASE_HOST` and `WEIR_DATABASE_SSLROOTCERT=/etc/weir/db-ca/ca.pem` are set
- **THEN** the address Weir connects with names that file as its root certificates

### Requirement: The database can be signed in to with an AWS IAM token
With `WEIR_DATABASE_IAM=aws`, Weir SHALL sign in to Postgres with no stored password. Each time it opens a connection it SHALL make a new Amazon RDS IAM token for the host, port and user of that connection and the region in `AWS_REGION`, signed with the AWS credentials of the place it runs, and use it as the password. `weir migrate` and every other command SHALL connect this way. A token MUST NOT appear in a log or an error.

#### Scenario: Each new connection has a token of its own
- **WHEN** Weir opens two connections to a database that accepts only the token made for each
- **THEN** a token is made twice, and both connections sign in

#### Scenario: The password changes while a connection is open
- **WHEN** a connection is open, and the password the database accepts then changes
- **THEN** the open connection still answers
- **AND** the next connection Weir opens signs in with a newly made token

#### Scenario: Migrating
- **WHEN** a migration is run against a database that accepts only a token
- **THEN** the migration signs in with a token and brings the schema up to date

#### Scenario: A token cannot be made
- **WHEN** Weir opens a connection and making the token fails
- **THEN** the connection is not opened, and the error says that a sign-in token could not be made, and why

#### Scenario: What a token is
- **WHEN** a token is made for the host `db.example.com`, port 5432, user `weir` and region `eu-west-2`
- **THEN** it names that host, port and user, is signed for `eu-west-2` and the service `rds-db`, and expires in 15 minutes

### Requirement: IAM sign-in is refused when a setting contradicts it
With `WEIR_DATABASE_IAM=aws`, Weir MUST refuse to start when a setting contradicts it: `WEIR_DATABASE_URL` or `WEIR_DATABASE_PASSWORD` is set, the host or user is missing, the host is a socket, no region is set, or `WEIR_DATABASE_SSLMODE` is not `verify-full`. With no mode set, the connection SHALL use `verify-full`. A value of `WEIR_DATABASE_IAM` other than `aws` MUST be refused. Each error SHALL name the setting to change.

#### Scenario: A password is set as well
- **WHEN** Weir is started with `WEIR_DATABASE_IAM=aws` and `WEIR_DATABASE_PASSWORD` set
- **THEN** it fails, and the error names `WEIR_DATABASE_PASSWORD`

#### Scenario: The certificate would not be checked
- **WHEN** Weir is started with `WEIR_DATABASE_IAM=aws` and `WEIR_DATABASE_SSLMODE=require`
- **THEN** it fails, and the error says the mode must be `verify-full`

#### Scenario: No mode is given
- **WHEN** Weir is started with `WEIR_DATABASE_IAM=aws`, a host, a user and a region, and no `WEIR_DATABASE_SSLMODE`
- **THEN** the address it connects with has `sslmode` set to `verify-full`, and holds no password

#### Scenario: No region
- **WHEN** Weir is started with `WEIR_DATABASE_IAM=aws` and neither `AWS_REGION` nor `AWS_DEFAULT_REGION`
- **THEN** it fails, and the error names `AWS_REGION`

#### Scenario: Another cloud is named
- **WHEN** Weir is started with `WEIR_DATABASE_IAM=gcp`
- **THEN** it fails, and the error says the only value is `aws`

### Requirement: The Helm chart supports IAM sign-in to the database
When `databaseCA.configMap` is set, the chart SHALL mount that ConfigMap in every container that opens the database, and set `WEIR_DATABASE_SSLROOTCERT` to the file in it. The chart MUST refuse to render when `env.WEIR_DATABASE_IAM` is set and `migrate.when` is `before`, because that Job runs with no AWS identity.

#### Scenario: The certificate authorities come from a ConfigMap
- **WHEN** the chart is rendered with `databaseCA.configMap: weir-db-ca`
- **THEN** the server's container mounts that ConfigMap and has `WEIR_DATABASE_SSLROOTCERT` set to `/etc/weir/db-ca/ca.pem`

#### Scenario: IAM sign-in with a migration Job
- **WHEN** the chart is rendered with `env.WEIR_DATABASE_IAM: aws` and `migrate.when: before`
- **THEN** rendering fails, and the error says to set `migrate.when` to `start`
