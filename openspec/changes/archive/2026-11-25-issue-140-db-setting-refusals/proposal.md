## Why

Two database settings can be ignored without a word. `WEIR_DATABASE_SSLROOTCERT` is read only when the database is given in parts, so with `WEIR_DATABASE_URL` it does nothing, and the Helm chart sets it whenever `databaseCA.configMap` is set. A user with one address believes the database's certificate is checked against their authorities when it is not. With `WEIR_DATABASE_IAM=aws`, a list of hosts in `WEIR_DATABASE_HOST` gets a token for the first host only, and the sign-in to any other fails at connection time, not at start. Every other contradiction in these settings is refused at start, with an error naming the setting.

Closes issue 140.

## What Changes

- Weir refuses to start when `WEIR_DATABASE_SSLROOTCERT` is set with `WEIR_DATABASE_URL`. The error says to put `sslrootcert=` in the address, or to give the database in parts.
- With `WEIR_DATABASE_IAM=aws`, Weir refuses a list of hosts in `WEIR_DATABASE_HOST`, or of ports in `WEIR_DATABASE_PORT`, because a token is signed for one host and port. The Postgres driver treats a comma in either as a list.
- The Helm chart's README and values say that `databaseCA` works only with the database given in parts.

Out of scope: other parts that are ignored when `WEIR_DATABASE_URL` is set (such as `WEIR_DATABASE_HOST`), which are documented as ignored; signing a token for each host in a list.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `deployment`: two requirements that issue 136 added are changed. Naming the certificate authorities for the database now refuses one address, and the settings IAM sign-in refuses now include a list of hosts or ports.

## Impact

- **Code:** `internal/config` only.
- **Dependencies:** none.
- **Rules that must not be weakened:** none is touched. Both changes refuse a setting that would otherwise be ignored.
- **Who is affected:** anyone who sets `WEIR_DATABASE_SSLROOTCERT` beside `WEIR_DATABASE_URL`, including through `databaseCA.configMap` in the chart, now fails at start where before the setting did nothing.
