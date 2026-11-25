## Why

The server reads its admin key only from `WEIR_ADMIN_KEY` in its environment. Some platforms deliver secrets as files instead: the Kubernetes Secrets Store CSI driver, or an init container that fetches a secret with the pod's own identity. Connection secrets can already come from files, but the admin key cannot, so it forces a long-lived Kubernetes Secret for this one value. A key in the environment is also easier to leak: it shows up in `/proc/<pid>/environ` and in anything that dumps the environment.

Closes issue 138.

## What Changes

- A new setting, `WEIR_ADMIN_KEY_FILE`, names a file that holds the admin key. `weir serve` reads it once at start. The key is the file's contents with surrounding whitespace trimmed, so a trailing newline does no harm.
- `weir serve` refuses to start, with an error naming the setting, when both `WEIR_ADMIN_KEY` and `WEIR_ADMIN_KEY_FILE` are set, when the file cannot be read, or when it holds nothing but whitespace. No error repeats the key.
- `WEIR_REQUIRE_ADMIN_KEY` is satisfied by either setting.
- The admin key settings are checked before the database is opened, so a server with a wrong setting stops at once, without waiting for a database.

Out of scope: reading the file again when it changes (a new key needs a restart); the Helm chart mounting the file, which is issue 139.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `access`: one requirement is added, for reading the admin key from a file, and "A server can be made to refuse to start open" is changed so that either setting satisfies it.

## Impact

- **Code:** `cmd/weir` reads and checks the two settings in one place, before it opens the database.
- **Dependencies:** none.
- **Rules that must not be weakened:** "Weir stores references to secrets, never secrets" is kept. The key is held in memory as before, and is never written to the database, a log or an error.
