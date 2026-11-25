## ADDED Requirements

### Requirement: The admin key can be read from a file
`WEIR_ADMIN_KEY_FILE` SHALL name a file holding the admin key, read once when `weir serve` starts. The key is the file's contents with surrounding whitespace trimmed, and it SHALL act as `WEIR_ADMIN_KEY` does. `weir serve` MUST refuse to start when both settings are set, when the file cannot be read, or when it holds only whitespace. Each error SHALL name the setting to change, and MUST NOT contain the key.

#### Scenario: The key is in a file
- **WHEN** `weir serve` is started with `WEIR_ADMIN_KEY_FILE` naming a file that holds the key followed by a newline
- **THEN** the admin key is the key without the newline

#### Scenario: Both settings are given
- **WHEN** `weir serve` is started with both `WEIR_ADMIN_KEY` and `WEIR_ADMIN_KEY_FILE` set
- **THEN** it exits with an error that names both settings, and does not contain either key

#### Scenario: The file is missing
- **WHEN** `weir serve` is started with `WEIR_ADMIN_KEY_FILE` naming a file that does not exist
- **THEN** it exits with an error that names `WEIR_ADMIN_KEY_FILE`

#### Scenario: The file is empty
- **WHEN** `weir serve` is started with `WEIR_ADMIN_KEY_FILE` naming a file that holds only a newline
- **THEN** it exits with an error that names `WEIR_ADMIN_KEY_FILE` and says the file is empty

## MODIFIED Requirements

### Requirement: A server can be made to refuse to start open
When `WEIR_REQUIRE_ADMIN_KEY` is set and neither `WEIR_ADMIN_KEY` nor `WEIR_ADMIN_KEY_FILE` is, `weir serve` MUST refuse to start and say why. The server's container image sets `WEIR_REQUIRE_ADMIN_KEY`. Without any of these settings the server SHALL start, and SHALL print a warning that anyone who can reach it can do anything.

#### Scenario: The image is started with no admin key
- **WHEN** `weir serve` is started with `WEIR_REQUIRE_ADMIN_KEY=1` and no `WEIR_ADMIN_KEY` or `WEIR_ADMIN_KEY_FILE`
- **THEN** it exits with an error that names all three settings

#### Scenario: The key comes from a file
- **WHEN** `weir serve` is started with `WEIR_REQUIRE_ADMIN_KEY=1` and `WEIR_ADMIN_KEY_FILE` naming a file that holds a key
- **THEN** the requirement is met, and the server goes on to start

#### Scenario: A laptop with no admin key
- **WHEN** `weir serve` is started with none of these settings
- **THEN** it starts, and prints a warning that no admin key is set
