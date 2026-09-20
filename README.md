# home-management-system

A terminal inventory of a house: what you own, where it is, and how much of it
is left. State is an event ledger, so every number on screen can be replayed
from the events that produced it.

## Where the data lives

Everything hms persists lives in one directory, `~/hms`:

```
~/hms/
  hms.db      the SQLite database
  .hms.json   configuration
```

One directory means "where is my data" has a single answer, and a backup is a
copy of one folder.

`.hms.json` is created on first run:

```json
{
  "db_file": "hms.db"
}
```

`db_file` names the database inside `~/hms`. An absolute path is taken as-is,
so a database can stay where it already is. Settings hms does not recognise are
preserved when the file is rewritten, so a newer version's keys survive an
older binary.

The database path is resolved in this order, and `hms --info` reports which one
won:

| Source | Notes |
| --- | --- |
| `--db-path FILE` | Highest. Does not read or create `~/hms`. |
| `$HMS_DB_PATH` | Same, for scripts. |
| `~/hms/.hms.json` | The normal case. Creates the directory and file if absent. |

`$HMS_HOME` moves the whole directory, which is how the tests avoid touching a
real one and how you would keep a second house.

`internal/config` is the only package that decides any of this — archlint
enforces it, so a second package cannot grow a second answer.

## Working on it

[mise](https://mise.jdx.dev) pins the toolchain and defines every command.
`mise install` once, then:

| Command | What it does |
| --- | --- |
| `mise run build` | Compile `hms` and `seed` into `bin/` |
| `mise run run -- --info` | Run hms from source |
| `mise run test` | Every test |
| `mise run lint` | archlint, golangci-lint, and `go vet` |
| `mise run format` | gofumpt over the tree |
| `mise run generate` | Regenerate sqlc output and `go:generate` |
| `mise run verify-generate` | Fail if generated files are stale |
| `mise run seed` / `reseed` | Build the sample house (reseed deletes first) |
| `mise run verify` | Integrity check against the configured database |
| `mise run setup` | Tidy, generate, build from a clean checkout |
| `mise run pre-commit` | The gate: format check, lint, build, test |
| `mise run ci` | What CI runs — pre-commit plus the staleness check |

`mise tasks` lists them with descriptions.

### The pre-commit hook

```
mise run hooks
```

writes a `.git/hooks/pre-commit` that runs `mise run pre-commit`. It is the same
task CI runs a superset of, so a green hook means a green build.

### CI

`.github/workflows/ci.yml` installs the toolchain with `jdx/mise-action` and
runs `mise run ci` — the same tools at the same versions a developer has, since
both read `mise.toml`.

## Architecture

`scripts/archlint.sh` (`mise run lint:arch`) enforces the boundaries the design
depends on: which package may write what, where transactions may begin, that
the domain is pure, and that storage location has one owner. Each rule is a
search that must find nothing. `docs/conceptual-schema.md` and
`docs/domain-model.md` are the long form.
