# file-store Specification

## Purpose
Weir has one place to keep files, for anything too large or too binary to sit in a step's output. Files an agent writes are kept there, for later steps of the run and for the application to collect.

## Requirements

### Requirement: Files are kept in Postgres unless a bucket is named
Weir SHALL keep files in the Postgres database it already uses, unless `WEIR_BLOB_URL` names a bucket, and then in that bucket. Every kind of storage SHALL behave the same. Weir holds no key for a bucket: the cloud's own library finds the credentials the platform provides.

#### Scenario: A file goes in and comes out
- **WHEN** a file is put under a key and then read back
- **THEN** the same bytes come back, and the put reported their size and SHA-256 digest
- **AND** putting again under the same key replaces the file

#### Scenario: A file that is not there
- **WHEN** a key with no file is read
- **THEN** it is reported as not found
- **AND** deleting a file that is not there is not an error

#### Scenario: A key that climbs out
- **WHEN** a file is put under a key that could name something outside the store, such as `acme/../rival/x`
- **THEN** the store refuses it

### Requirement: Only known kinds of bucket open
`WEIR_BLOB_URL` SHALL be an address that begins `s3://`, `gs://`, `azblob://` or `file://`. The server SHALL refuse to start with any other address. A build made with the `nobuckets` tag has no bucket support and SHALL refuse to start when `WEIR_BLOB_URL` is set. Where Weir says which storage it is using, it SHALL leave out whatever follows a `?` in the address.

#### Scenario: An address that is not a bucket
- **WHEN** the address is `http://example.com/bucket`
- **THEN** it does not open

#### Scenario: An address with settings
- **WHEN** a bucket is opened at an address that ends `?metadata=skip`
- **THEN** its description shows the address without the settings

### Requirement: A file that is too large is refused and nothing of it is kept
A store SHALL accept a file of at most 32 MB. A larger one SHALL be refused as too large, and no part of it SHALL remain in the store.

#### Scenario: A file over the limit
- **WHEN** a file larger than the store's limit is put
- **THEN** the put fails as too large
- **AND** reading the key afterwards finds no file

### Requirement: Files an agent writes are kept for later steps
When an `ai` step with `writes:` completes, Weir SHALL keep each file its agent wrote in the file store, under the run's own workspace, with its path, size, SHA-256 digest and content type. A later `ai` step that lists `files: [{step: <id>}]` SHALL be given every one of those files, under the paths they were written at. Expressions SHALL see them as `steps.<id>.files`, with no contents and no storage key.

#### Scenario: A review step reads a draft step's files
- **WHEN** the step `draft` writes three files and the step `review` lists `files: [{step: draft}]`
- **THEN** the agent for `review` is given the three files, with their paths and contents

#### Scenario: An expression asks what was written
- **WHEN** a later step's input is `steps.draft.files`
- **THEN** it is a list of `path`, `size`, `sha256`, `content_type` and `deleted` for each file, and nothing of where it is stored

#### Scenario: A step deleted a file
- **WHEN** a step deletes a file from a prepared directory
- **THEN** the file is listed with `deleted: true` and no contents

### Requirement: A step may only read the files of a step that writes
Publishing SHALL refuse a step whose `files` names a step that has no `writes:`, one that names the step itself, and an entry that names a step and also sets fields that belong to a fetched document. It SHALL refuse a `writes:` whose limits are out of range.

#### Scenario: The named step writes nothing
- **WHEN** a definition has `files: [{step: draft}]` and `draft` has no `writes:`
- **THEN** publishing is refused, saying `draft` is not a step that writes files

### Requirement: A step that cannot keep its files fails
When a step's agent wrote files and the server has no file store, or a file cannot be kept, the step SHALL fail and say why. It MUST NOT complete with files missing.

#### Scenario: No file store
- **WHEN** a step's agent writes a file on a server with no file store
- **THEN** the run does not succeed, and the error says there is no file store

### Requirement: A run's files can be listed and fetched over the API
`GET /v1/runs/{id}/files` SHALL list the files a run's steps produced, and `GET /v1/runs/{id}/files/{step}/{path}` SHALL send one file. A file SHALL always be sent as a download, so that a browser never runs it as a page. Both are scoped to the workspace: another workspace SHALL get 404. A file that was never written, or was deleted, SHALL get 404.

#### Scenario: The application lists a run's files
- **WHEN** the application lists the files of a run whose step `draft` wrote three
- **THEN** the list holds three, each with its step, path, size, digest and content type

#### Scenario: The application collects a file
- **WHEN** the application fetches `notes/a file with spaces.txt` of the step `draft`
- **THEN** the reply is 200 with the file's bytes
- **AND** it carries `Content-Disposition: attachment`, `X-Content-Type-Options: nosniff` and the file's digest in `Weir-File-Sha256`

#### Scenario: Another workspace asks
- **WHEN** the workspace `rival` lists or fetches the files of another workspace's run
- **THEN** the reply is 404

#### Scenario: A file that is not there
- **WHEN** the application fetches a file the step never wrote, or one it deleted
- **THEN** the reply is 404

### Requirement: A working directory is packed to send to an agent and unpacked as it was
Weir SHALL pack a working directory into one compressed archive to send to an agent in a container of its own. Unpacking SHALL bring back ordinary files with their contents and whether they are executable, directories including empty ones, and links. Packing SHALL fail when the archive would be larger than the limit it is given.

#### Scenario: A directory there and back
- **WHEN** a directory with a nested file, an executable script, a link and an empty directory is packed and unpacked
- **THEN** all four are there as they were

#### Scenario: A directory over the limit
- **WHEN** a directory packs to more than the limit
- **THEN** packing fails as too large

### Requirement: An archive cannot write outside its directory
Unpacking SHALL refuse an archive that names a path outside the directory it is unpacked into, and SHALL write nothing there.

#### Scenario: An entry that climbs out
- **WHEN** an archive holds an entry named `../escape.txt`
- **THEN** unpacking fails and no file is written outside the directory
