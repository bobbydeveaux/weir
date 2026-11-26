## ADDED Requirements

### Requirement: How many steps a server runs at once is a setting
`WEIR_WORKER_CONCURRENCY` SHALL set how many steps a server's worker runs at once. Left out, it is 1. A value that is not a whole number from 1 to 256 MUST be refused at start, and the error SHALL name `WEIR_WORKER_CONCURRENCY`.

#### Scenario: A number is given
- **WHEN** `weir serve` is started with `WEIR_WORKER_CONCURRENCY=8`
- **THEN** its worker runs up to eight steps at once

#### Scenario: Nothing is given
- **WHEN** `weir serve` is started without `WEIR_WORKER_CONCURRENCY`
- **THEN** its worker runs one step at a time

#### Scenario: A value that cannot be right
- **WHEN** `weir serve` is started with `WEIR_WORKER_CONCURRENCY` set to `0`, `-1`, `two` or `1000`
- **THEN** it fails, and the error names `WEIR_WORKER_CONCURRENCY`

### Requirement: The Helm chart sets how many steps each server runs at once
The chart SHALL have a value `worker.concurrency`, 1 by default, and SHALL set `WEIR_WORKER_CONCURRENCY` from it in the server's container, and not in the migration's.

#### Scenario: The value is raised
- **WHEN** the chart is rendered with `worker.concurrency=4`
- **THEN** the server's container has `WEIR_WORKER_CONCURRENCY` set to `4`
