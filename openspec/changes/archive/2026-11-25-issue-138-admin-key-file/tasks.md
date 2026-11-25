## 1. Settings

- [x] 1.1 Read `WEIR_ADMIN_KEY` and `WEIR_ADMIN_KEY_FILE` in one place in `cmd/weir`, trimming the file's contents
- [x] 1.2 Refuse both settings together, a file that cannot be read, and an empty file, each naming the setting and never the key
- [x] 1.3 Let either setting satisfy `WEIR_REQUIRE_ADMIN_KEY`, and check them before the database is opened
- [x] 1.4 Say so in `weir help`

## 2. Tests

- [x] 2.1 The key from a file, both settings together, a missing file, an empty file, and the requirement met by either setting
- [x] 2.2 `weir serve` refusing each of those before it needs a database

## 3. Finish

- [x] 3.1 `README.md` and `docs/deploy/your-own-platform.md`
- [x] 3.2 `docs/build/02-epics.md`
- [x] 3.3 `openspec/specs/access/proof.md`
- [x] 3.4 Archive the change, then `make check` and `make spec`
