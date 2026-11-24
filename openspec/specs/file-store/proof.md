# Proof: file-store

Which test proves each requirement. `make spec` checks that every requirement is listed here and that every test named exists.

| Requirement | Proven by |
| --- | --- |
| Files are kept in Postgres unless a bucket is named | `blob/postgres_test.go:TestPostgresKeepsFiles`, `blob/bucket/bucket_test.go:TestADirectoryKeepsFiles`, `blob/bucket/bucket_test.go:TestARealBucketKeepsFiles` (Postgres and a directory run in CI; the real bucket test runs only when one is named, and has been run for S3 against an S3-compatible server only, never for Google Cloud Storage or Azure; choosing the store from `WEIR_BLOB_URL`, in `cmd/weir/main.go`, is not tested) |
| Only known kinds of bucket open | `blob/bucket/bucket_test.go:TestOnlyKnownKindsOfBucketOpen` (the refused addresses and the description are asserted; the `nobuckets` build is not tested) |
| A file that is too large is refused and nothing of it is kept | `blob/postgres_test.go:TestPostgresKeepsFiles`, `blob/bucket/bucket_test.go:TestADirectoryKeepsFiles` (with a store whose limit is 1 KB; the default of 32 MB is not asserted) |
| Files an agent writes are kept for later steps | `api/files_test.go:TestAnAgentsFilesAreKeptForLaterStepsAndTheApplication`, `api/workspace_test.go:TestAStepsDirectoryIsPreparedAndItsChangesAreKept` |
| A step may only read the files of a step that writes | `api/files_test.go:TestAStepMayOnlyReadTheFilesOfAStepThatWrites` |
| A step that cannot keep its files fails | `api/files_test.go:TestAStepThatCannotKeepItsFilesFails` (a server with no file store is asserted; a store that refuses one file is not) |
| A run's files can be listed and fetched over the API | `api/files_test.go:TestAnAgentsFilesAreKeptForLaterStepsAndTheApplication`, `api/workspace_test.go:TestAStepsDirectoryIsPreparedAndItsChangesAreKept` |
| A working directory is packed to send to an agent and unpacked as it was | `internal/archive/archive_test.go:TestADirectoryComesBackAsItWas` |
| An archive cannot write outside its directory | `internal/archive/archive_test.go:TestAnArchiveCannotWriteOutsideItsDirectory` |
