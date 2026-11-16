# Contributing to Weir

Weir is at an early stage, and the workflow contract is still changing. The best time to shape it is now.

## Before you start

Open an issue describing what you want to change before writing much code. The contract is deliberately small, and a short conversation first saves a rejected pull request.

## Making a change

1. Fork the repository and create a branch.
2. Run `make check`. It must pass: formatting, `go vet`, and every test against Postgres.
3. Add tests for what you change. Tests for the store run against a real Postgres, not a fake.
4. Open a pull request that says what changed and why.

## What we look for

- **Small, focused changes.** One idea per pull request.
- **Tests that would fail without the change.**
- **Plain names and comments that say why,** not what.
- **No new dependency without a reason** in the pull request description.

## Licence

By contributing you agree that your work is licensed under Apache 2.0, the same as the rest of the project.
