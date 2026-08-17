.PHONY: build run test lint generate verify-generate verify seed deps clean setup check fmt

DB_PATH ?= ./hms.db

## build: compile to bin/hms
build:
	go build -o bin/hms ./cmd/hms
	go build -o bin/seed ./cmd/seed

## run: run the application directly
run:
	go run ./cmd/hms --db-path=$(DB_PATH)

## test: run every test
test:
	go test ./...

## lint: enforce the architectural boundaries (plan §1.1)
lint:
	@./scripts/archlint.sh

## generate: regenerate SQLC and any go:generate output
generate:
	@if ls internal/db/queries/*.sql >/dev/null 2>&1; then \
		sqlc generate; \
	else \
		echo "sqlc: no query files yet — skipping (activates in Stage 2)"; \
	fi
	go generate ./...

## verify-generate: fail if generated files are stale
##
## The generated registry only closes the exhaustiveness hole if generation
## actually runs. Without this, adding an event type and forgetting to
## regenerate leaves a stale registry that hides it — the very gap the
## generator exists to prevent.
##
## Uses git status rather than git diff: diff does not report UNTRACKED files,
## so a newly generated file that was never committed would slip through.
verify-generate:
	@$(MAKE) --no-print-directory generate >/dev/null
	@dirty="$$(git status --porcelain -- '*_gen.go' internal/db/sqlc internal/db/probe)"; \
	if [ -n "$$dirty" ]; then \
		echo "generated files are stale or uncommitted — run 'make generate' and commit:" >&2; \
		echo "$$dirty" >&2; \
		exit 1; \
	fi
	@echo "generated files are up to date"

## seed: build a sample house, entirely through the real write paths
seed:
	go run ./cmd/seed --db-path=$(DB_PATH)

## verify: run the integrity check against DB_PATH
verify:
	go run ./cmd/hms --db-path=$(DB_PATH) --verify

## fmt: format and vet
fmt:
	go fmt ./...
	go vet ./...

## deps: tidy the module
deps:
	go mod tidy

## setup: full build from a clean checkout
setup: deps generate build

## check: everything CI runs
check: fmt lint verify-generate test

## clean: remove build artifacts
clean:
	rm -rf bin/
