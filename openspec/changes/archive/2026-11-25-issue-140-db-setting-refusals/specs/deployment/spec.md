## MODIFIED Requirements

### Requirement: The certificate authorities for the database can be named
When the database is given in parts, `WEIR_DATABASE_SSLROOTCERT` SHALL name a file of certificate authorities, and Weir SHALL check the database's certificate against that file. With `WEIR_DATABASE_URL`, where it would be ignored, Weir MUST refuse to start when `WEIR_DATABASE_SSLROOTCERT` is set, and the error SHALL say to put `sslrootcert=` in the address or give the database in parts.

#### Scenario: A file of certificate authorities is named
- **WHEN** `WEIR_DATABASE_HOST` and `WEIR_DATABASE_SSLROOTCERT=/etc/weir/db-ca/ca.pem` are set
- **THEN** the address Weir connects with names that file as its root certificates

#### Scenario: A file of certificate authorities beside one address
- **WHEN** Weir is started with `WEIR_DATABASE_URL` and `WEIR_DATABASE_SSLROOTCERT` set
- **THEN** it fails, and the error names `WEIR_DATABASE_SSLROOTCERT` and says to put `sslrootcert=` in `WEIR_DATABASE_URL` or give the database in parts

### Requirement: IAM sign-in is refused when a setting contradicts it
With `WEIR_DATABASE_IAM=aws`, Weir MUST refuse to start on any of these: `WEIR_DATABASE_URL` or `WEIR_DATABASE_PASSWORD` is set, the host or user is missing, the host is a socket, the host or port is a list, no region is set, or `WEIR_DATABASE_SSLMODE` is not `verify-full`. With no mode set, the connection SHALL use `verify-full`. A value of `WEIR_DATABASE_IAM` other than `aws` MUST be refused. Each error SHALL name the setting to change.

#### Scenario: A password is set as well
- **WHEN** Weir is started with `WEIR_DATABASE_IAM=aws` and `WEIR_DATABASE_PASSWORD` set
- **THEN** it fails, and the error names `WEIR_DATABASE_PASSWORD`

#### Scenario: A list of hosts
- **WHEN** Weir is started with `WEIR_DATABASE_IAM=aws` and `WEIR_DATABASE_HOST=db-a.example.com,db-b.example.com`
- **THEN** it fails, and the error names `WEIR_DATABASE_HOST` and says a token is signed for one host

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
