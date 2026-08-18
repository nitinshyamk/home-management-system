#!/usr/bin/env bash
#
# archlint — enforces the architectural boundaries from the v01 plan §1.1.
#
# These rules are prose promises unless something checks them. Each one below is
# a property the design depends on, expressed as a search that must find nothing.
#
# Rules whose target does not exist yet report as SKIPPED, never as passing. A
# green tick for a directory that is not there is exactly the false assurance
# these rules exist to prevent.
#
# Usage: scripts/archlint.sh        (exits non-zero on any violation)

set -uo pipefail
cd "$(dirname "$0")/.."

fail=0
violations=0

report() {
  printf '  \033[31m✗\033[0m %s\n' "$1" >&2
  violations=$((violations + 1))
  fail=1
}

ok() { printf '  \033[32m✓\033[0m %s\n' "$1"; }

# A rule whose target does not exist yet has not been checked. Saying so is the
# whole point: a green tick for a directory that is not there is precisely the
# false assurance these rules exist to prevent.
skip() { printf '  \033[90m·\033[0m %s \033[90m(%s not present yet)\033[0m\n' "$1" "$2"; }

# rule <description> <dir> <glob> <pattern>
# Fails if the pattern is found anywhere under dir. Skips, loudly, if dir is absent.
rule() {
  local desc="$1" dir="$2" glob="$3" pattern="$4"
  if [ ! -d "$dir" ]; then
    skip "$desc" "$dir"
    return
  fi
  local hits
  hits="$(grep -rniE --include="$glob" "$pattern" "$dir" 2>/dev/null)"
  if [ -n "$hits" ]; then
    report "$desc"
    printf '      %s\n' "$hits" >&2
  else
    ok "$desc"
  fi
}

echo "archlint: schema rules"

# Nothing is ever hard-deleted (E4, L3), so a cascade clause can never fire.
# It is documentation pretending to be behaviour, and a live footgun the day a
# delete path appears. Every foreign key must be RESTRICT.
rule "migrations use RESTRICT, never CASCADE" \
  internal/db/migrations '*.sql' 'on (delete|update) cascade'

# A Down section that is missing or empty makes the round-trip test vacuous.
if [ -d internal/db/migrations ]; then
  missing=""
  for f in internal/db/migrations/*.sql; do
    [ -e "$f" ] || continue
    if ! grep -q -- '-- +goose Down' "$f"; then
      missing="${missing}${f}: no Down section"$'\n'
    elif [ -z "$(sed -n '/-- +goose Down/,$p' "$f" | tail -n +2 | grep -v '^\s*$' | grep -v '^\s*--')" ]; then
      missing="${missing}${f}: empty Down section"$'\n'
    fi
  done
  if [ -n "$missing" ]; then
    report "every migration has a non-empty Down section"
    printf '      %s' "$missing" >&2
  else
    ok "every migration has a non-empty Down section"
  fi
fi

# sqlc v1.30.0 truncates a generated statement by 2 bytes for every multi-byte
# character in the comment preceding it -- a rune/byte offset bug. The result is
# syntactically invalid SQL that compiles fine and fails at runtime with
# "incomplete input". Keeping .sql files ASCII-only sidesteps it entirely.
#
# Reproduced minimally: one em dash in a leading comment drops the last two
# characters of the statement; three drop six.
if ls internal/db/queries/*.sql internal/db/probe/*.sql internal/db/migrations/*.sql >/dev/null 2>&1; then
  nonascii="$(LC_ALL=C grep -rln '[^ -~	]' internal/db/queries internal/db/probe internal/db/migrations --include='*.sql' 2>/dev/null || true)"
  if [ -n "$nonascii" ]; then
    report "SQL files are ASCII-only (sqlc truncates statements after multi-byte comment characters)"
    printf '      %s\n' "$nonascii" >&2
  else
    ok "SQL files are ASCII-only"
  fi
fi

echo "archlint: write-path boundaries (plan §1.1)"

# Queries live centrally in internal/db/queries and are named for the path that
# owns them, so the boundary is checked by FILE PREFIX, not by directory. An
# earlier version of this script checked internal/origin/*.sql — a directory that
# holds Go, not SQL — so the rules could never fire.
#
# rule_prefix <description> <prefix> <pattern>
rule_prefix() {
  local desc="$1" prefix="$2" pattern="$3"
  if ! ls internal/db/queries/${prefix}_*.sql >/dev/null 2>&1; then
    skip "$desc" "internal/db/queries/${prefix}_*.sql"
    return
  fi
  local hits
  hits="$(grep -niE "$pattern" internal/db/queries/${prefix}_*.sql 2>/dev/null)"
  if [ -n "$hits" ]; then
    report "$desc"
    printf '      %s\n' "$hits" >&2
  else
    ok "$desc"
  fi
}

# Origination writes immutable birth facts and nothing else. It creates rows; it
# never revises them. An origination error is permanent — the only remedy is
# retiring the entity — so the path must have no revision statement at all.
rule_prefix "origin_*.sql contains no UPDATE" origin '^\s*update\s'
rule_prefix "origin_*.sql contains no DELETE" origin '^\s*delete\s+from'

# Annotation revises directly-mutable attributes on rows that already exist.
# It never brings anything into being.
rule_prefix "annotate_*.sql contains no INSERT" annotate '^\s*insert\s+into'

# Query reads. That is all.
rule_prefix "query_*.sql contains no writes" query '^\s*(insert|update|delete)\s'

echo "archlint: ledger ownership"

# Query files are named for the path that owns them, which lets the ledger-owned
# statements be identified by filename. Any query file outside that convention
# has no owner, and no owner means no boundary.
if [ -d internal/db/queries ] && ls internal/db/queries/*.sql >/dev/null 2>&1; then
  unowned=""
  for f in internal/db/queries/*.sql; do
    [ -e "$f" ] || continue
    case "$(basename "$f")" in
      origin_*|ledger_*|annotate_*|query_*) ;;
      *) unowned="${unowned}${f}"$'\n' ;;
    esac
  done
  if [ -n "$unowned" ]; then
    report "every query file is prefixed with its owning path"
    printf '      %s' "$unowned" >&2
  else
    ok "every query file is prefixed with its owning path"
  fi
else
  skip "every query file is prefixed with its owning path" "internal/db/queries/*.sql"
fi

# Methods generated from <path>_*.sql may only be called from internal/<path>.
# This is the rule that makes "the ledger is the only writer of ledger-derived
# columns" mechanically true rather than aspirational, and the same argument
# applies to every path, so it is applied uniformly.
#
# Only files that IMPORT internal/db/sqlc are searched. A generated query cannot
# be called without importing the package, and the owning packages legitimately
# expose wrapper methods of the same name -- query.Reader.CountItemsInCategoryTree
# wraps the query of that name -- so a bare grep for the identifier flags callers
# of the wrapper, which is exactly the correct usage.
sqlc_files="$(grep -rl 'home-management-system/internal/db/sqlc' --include='*.go' internal/ cmd/ 2>/dev/null || true)"

for path in origin ledger annotate query; do
  if ! ls internal/db/queries/${path}_*.sql >/dev/null 2>&1; then
    skip "${path}-owned queries are called only from internal/${path}" "internal/db/queries/${path}_*.sql"
    continue
  fi
  if [ -z "$sqlc_files" ]; then
    skip "${path}-owned queries are called only from internal/${path}" "no sqlc importers"
    continue
  fi

  # Files that import sqlc but live outside the owning package.
  foreign="$(printf '%s\n' "$sqlc_files" \
             | grep -v "^internal/${path}/" \
             | grep -v '^internal/db/sqlc/' \
             | grep -v '_test\.go$' || true)"

  leaked=""
  if [ -n "$foreign" ]; then
    while IFS= read -r name; do
      [ -n "$name" ] || continue
      hits="$(printf '%s\n' "$foreign" | xargs grep -n "\.${name}(" 2>/dev/null || true)"
      [ -n "$hits" ] && leaked="${leaked}${hits}"$'\n'
    done < <(grep -ho -- '-- name: [A-Za-z0-9_]*' internal/db/queries/${path}_*.sql | sed 's/-- name: //')
  fi

  if [ -n "$leaked" ]; then
    report "${path}-owned queries are called only from internal/${path}"
    printf '      %s' "$leaked" >&2
  else
    ok "${path}-owned queries are called only from internal/${path}"
  fi
done

echo "archlint: package purity"

# The probe package exists only to attempt writes the schema must reject.
# Production code importing it would mean production code attempting them.
#
# Uses `go list` rather than grep: sqlc copies SQL comments into the generated
# Go, so a prose mention of the import path is not an import. .Imports excludes
# test-only imports, which is exactly the distinction the rule needs.
if [ -d internal/db/probe ] && command -v go >/dev/null 2>&1; then
  hits="$(go list -f '{{.ImportPath}}{{range .Imports}} {{.}}{{end}}' ./... 2>/dev/null \
          | grep 'home-management-system/internal/db/probe' \
          | grep -v '^home-management-system/internal/db/probe ' || true)"
  if [ -n "$hits" ]; then
    report "internal/db/probe is imported only from tests"
    printf '      %s\n' "$hits" >&2
  else
    ok "internal/db/probe is imported only from tests"
  fi
else
  skip "internal/db/probe is imported only from tests" "internal/db/probe"
fi

# The domain package is pure: types, the fold, and invariant predicates. If it
# grows a dependency it has grown I/O, and the fold stops being trivially
# testable and trivially replayable.
if [ -d internal/domain ] && command -v go >/dev/null 2>&1; then
  deps="$(go list -deps ./internal/domain 2>/dev/null | grep -E '^(home-management-system|github\.com|golang\.org|gopkg\.in)' | grep -v '^home-management-system/internal/domain' || true)"
  if [ -n "$deps" ]; then
    report "internal/domain imports stdlib only"
    printf '      %s\n' "$deps" >&2
  else
    ok "internal/domain imports stdlib only"
  fi
else
  skip "internal/domain imports stdlib only" "internal/domain"
fi

echo "archlint: transaction ownership"

# grep_go <description> <pattern> <allowed-dir>...
# Fails if the pattern appears in any non-test Go file outside the allowed dirs.
# Tests are exempt: several of them exist precisely to exercise the arrangement
# these rules forbid, so that the rules are known to be forbidding something.
grep_go() {
  local desc="$1" pattern="$2"; shift 2
  local hits
  hits="$(grep -rnE --include='*.go' -- "$pattern" internal cmd tools 2>/dev/null | grep -v '_test\.go:')"
  local dir
  for dir in "$@"; do
    hits="$(printf '%s\n' "$hits" | grep -v "^${dir}/" || true)"
  done
  hits="$(printf '%s' "$hits" | grep -v '^$' || true)"
  if [ -n "$hits" ]; then
    report "$desc"
    printf '      %s\n' "$hits" >&2
  else
    ok "$desc"
  fi
}

# The single most important boundary in the write layer. Every other package
# reaches a transaction through db.Scope.Run, which is the ONE place that
# decides whether a unit of work begins a transaction or joins one already in
# flight. A second such decision anywhere else means two units of work that
# each believe they are atomic, which is the defect v01 shipped with.
grep_go "database/sql transactions begin only in internal/db" \
        '\.BeginTx\(' internal/db

grep_go "db.InTx is called only from internal/db" \
        'db\.InTx\(' internal/db

# db.Enlist turns a raw transaction into a write-path scope. Only the three
# write paths may do it, in their NewTx constructors. Anywhere else would mean
# something outside the write layer deciding what a unit of work contains.
grep_go "db.Enlist appears only in the write paths" \
        'db\.Enlist\(' internal/origin internal/ledger internal/annotate

# ops is the only package that assembles writes across paths. If the UI could
# call a write path directly it would bypass the transaction that makes an
# intent atomic -- and bypass the review that makes origination deliberate.
if command -v go >/dev/null 2>&1; then
  for pkg in tui command; do
    if [ ! -d "internal/$pkg" ]; then
      skip "internal/$pkg does not import a write path" "internal/$pkg"
      continue
    fi
    hits="$(go list -f '{{range .Imports}}{{.}}
{{end}}' ./internal/$pkg/... 2>/dev/null \
            | grep -E 'internal/(origin|ledger|annotate)$' | sort -u || true)"
    if [ -n "$hits" ]; then
      report "internal/$pkg does not import a write path"
      printf '      %s\n' "$hits" >&2
    else
      ok "internal/$pkg does not import a write path"
    fi
  done
fi

echo
if [ "$fail" -ne 0 ]; then
  printf '\033[31marchlint: %d violation(s)\033[0m\n' "$violations" >&2
  exit 1
fi
printf '\033[32marchlint: clean\033[0m\n'
