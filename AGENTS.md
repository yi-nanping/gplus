# Repository Guidelines

## Project Structure & Module Organization

`gplus` is a Go 1.24+ library extending GORM with generic repositories and type-safe query builders. Source files live in the root package: `repository.go` provides CRUD operations; `query.go`, `update.go`, and `builder.go` build statements; `schema.go`, `alias.go`, and `subquery.go` handle model metadata and advanced queries.

Driver-free unit tests sit beside source as `*_test.go`. Database tests and examples live in the independent `tests/` module, which owns database driver dependencies and replaces the library with local source. Consult `README.md`, `doc.go`, and `CLAUDE.md` for guidance. Setup notes live in `docs/dev-setup/`; CI lives in `.github/workflows/ci.yml`.

## Build, Test, and Development Commands

Run commands from the repository root:

- `go build ./...` — compile the library.
- `go test ./...` — run driver-free unit tests.
- `cd tests` then `go test ./...` — run database tests (SQLite by default).
- From `tests/`: `go test -run TestRepository_CRUD_And_Errors ./...` — run a focused database test.
- `go vet ./...` — run static checks used by CI.
- `go test -v -race ./...` — run unit tests with race detection; also run this command in `tests/` to match both CI gates.
- `go run ./scripts/check-driver-deps.go` — verify independent consumers compile with only their selected driver.
- `go test -coverprofile=coverage.out ./...` followed by `go tool cover -func=coverage.out` — inspect coverage.

## Coding Style & Naming Conventions

Format changed Go files with `gofmt`; use its tab indentation and standard import grouping. Use exported `PascalCase` names, unexported `camelCase` names, and descriptive lowercase filenames. Follow existing Chinese comments and document exported APIs. Keep changes focused and preserve public API compatibility.

Field pointers must come from the model instance returned by `NewQuery[T]` or `NewUpdater[T]`. Preserve data-rule enforcement and build-time error rejection when adding execution paths.

## Testing Guidelines

Use Go's `testing` package, `TestFeature_Scenario` functions, and `BenchmarkFeature` benchmarks. Add regression tests for fixes, checking SQL and observable database effects where relevant.

Database tests in `tests/` default to in-memory SQLite when database environment variables are unset. Explicit `TEST_DB=mysql|pg` requires a working DSN; unconfigured optional suites skip. CI supplies both services. From `tests/`, Oracle and DM suites require `go test -tags=oracle ./...` or `go test -tags=dm ./...`. Report skipped suites explicitly. Root SQL construction tests use a driver-free Dialector and do not prove real database behavior. CI has no enforced coverage percentage; maintain meaningful coverage of changed behavior.

Do not import database drivers into the core module, including its tests. Run `go mod tidy -diff` in both modules; do not add test-module replacements to the published core module. Root `go test ./...` does not include the nested test module.

## Commit & Pull Request Guidelines

History uses prefixes such as `fix:`, `docs:`, and `chore:` with descriptive Chinese summaries. Include a structured body explaining changes, reasons, and validation. PR descriptions should identify the problem, resulting behavior, related issues when applicable, and executed checks, including database coverage and skips. Update examples or documentation for API changes.

## Security & Configuration

Use disposable databases: integration tests migrate and clear tables. Configure credentials through `TEST_MYSQL_DSN`, `TEST_PG_DSN`, `TEST_ORACLE_DSN`, or `TEST_DM_DSN`; never commit real credentials or use production databases for tests.
