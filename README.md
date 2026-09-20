# home-management-system

A terminal inventory of a house: what you own, where it is, and how much of it
is left. State is an event ledger, so every number on screen can be replayed
from the events that produced it.

## Installing it

```
mise run install
```

builds `hms` and puts it in `~/.local/bin`, or wherever `$HMS_INSTALL_DIR`
points. It says so if that directory is not on your `PATH`. `mise run uninstall`
removes it.

Only `hms` is installed. `hmsdev` — the frame renderer, the schema printer, the
headless binder — belongs to the checkout, and `mise run render` is how it is
reached.

## Using it

```
hms                 open the house
hms import [NAME]   walk a bulk import
hms verify          check stored state against the ledger
hms checkpoint      record replay checkpoints
hms info            say which database is open, and what decided that
```

`--db-path FILE` opens a different database. That is the whole command line:
everything else hms can be made to do is a way of testing it rather than a way
of using it, and lives in `hmsdev`.

### `hms import`

A bulk import is a folder, not a command. `hms import` makes one under
`~/hms/imports`, asking what to call it:

```
~/hms/imports/march-receipt/
  schema/   the contract, written against the house as it is right now
  input/    what you are importing: a text file, a photograph, a note
  plan/     the rows that come back, which is what hms reviews
```

Three subdirectories because the three files have three different authors.
`schema/` is written by hms and read by an agent; `input/` is written by you and
read by an agent; `plan/` is written by an agent and read by hms.

The workflow then waits. Put the receipt in `input/` — in a file manager, in
another terminal, however you like — and press enter. hms produces the plan,
checks it reads as commands, and opens the review screen on it, where nothing is
applied until you approve it.

`schema/handoff.md` is what an agent is given: the contract, plus the two paths
and the instruction not to apply anything.

Naming an import again resumes it, which is how a workflow with a pause in the
middle is picked up the next day. If a plan is already there, hms offers it.

**The planner.** Turning a photograph of a receipt into rows is a reading
problem, and hms is an inventory. So the step is a command you configure:

```json
{
  "db_file": "hms.db",
  "import_planner": "my-agent \"$HMS_HANDOFF_FILE\""
}
```

It runs in the import directory, with the handoff on stdin and the paths in the
environment (`HMS_IMPORT_DIR`, `HMS_SCHEMA_DIR`, `HMS_HANDOFF_FILE`,
`HMS_INPUT_DIR`, `HMS_PLAN_FILE`), and is expected to write `$HMS_PLAN_FILE`.

hms ships no default, deliberately: a program that silently shelled out to
whatever agent happened to be installed would be sending a photograph of your
kitchen somewhere you never named. With nothing configured — and if the
configured planner fails — the workflow prints what needs doing and waits for
the plan file, which is the same workflow with you doing that step.

## Where the data lives

Everything hms persists lives in one directory, `~/hms`:

```
~/hms/
  hms.db      the SQLite database
  .hms.json   configuration
  imports/    one folder per bulk import
```

One directory means "where is my data" has a single answer, and a backup is a
copy of one folder.

`.hms.json` is created on first run:

```json
{
  "db_file": "hms.db",
  "import_planner": ""
}
```

`db_file` names the database inside `~/hms`. An absolute path is taken as-is,
so a database can stay where it already is. Settings hms does not recognise are
preserved when the file is rewritten, so a newer version's keys survive an
older binary.

The database path is resolved in this order, and `hms info` reports which one
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
| `mise run install` | Build `hms` and put it on `PATH` |
| `mise run uninstall` | Remove the installed `hms` |
| `mise run build` | Compile `hms`, `hmsdev` and `seed` into `bin/` |
| `mise run run -- info` | Run hms from source |
| `mise run render -- script.keys` | Replay a key script and print each frame |
| `mise run schema -- --format json` | Print the agent contract |
| `mise run plan -- file.csv` | Bind a plan and print it as JSON |
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
