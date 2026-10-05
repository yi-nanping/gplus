# Repository Guidelines

## Project Structure & Module Organization

`gplus` is a Go 1.24+ library extending GORM with generic repositories and type-safe query builders. Source files live in the root package: `repository.go` provides CRUD operations; `query.go`, `update.go`, and `builder.go` build statements; `schema.go`, `alias.go`, and `subquery.go` handle model metadata and advanced queries.

Tests sit beside source as `*_test.go`; `example_test.go` contains examples. Consult `README.md`, `doc.go`, and `CLAUDE.md` for guidance. Setup notes live in `docs/dev-setup/`; CI lives in `.github/workflows/ci.yml`.

## Build, Test, and Development Commands

Run commands from the repository root:

- `go build ./...` — compile the library.
- `go test ./...` — run the default test suite.
- `go test -run TestRepository_CRUD_And_Errors ./...` — run a focused test.
- `go vet ./...` — run static checks used by CI.
- `go test -v -race ./...` — run verbose tests with race detection, matching CI.
- `go test -coverprofile=coverage.out ./...` followed by `go tool cover -func=coverage.out` — inspect coverage.

## Coding Style & Naming Conventions

Format changed Go files with `gofmt`; use its tab indentation and standard import grouping. Use exported `PascalCase` names, unexported `camelCase` names, and descriptive lowercase filenames. Follow existing Chinese comments and document exported APIs. Keep changes focused and preserve public API compatibility.

Field pointers must come from the model instance returned by `NewQuery[T]` or `NewUpdater[T]`. Preserve data-rule enforcement and build-time error rejection when adding execution paths.

## Testing Guidelines

Use Go's `testing` package, `TestFeature_Scenario` functions, and `BenchmarkFeature` benchmarks. Add regression tests for fixes, checking SQL and observable database effects where relevant.

Shared tests default to in-memory SQLite when database environment variables are unset. MySQL and PostgreSQL integration tests skip unavailable databases; CI supplies both services. Oracle and DM suites require `go test -tags=oracle ./...` or `go test -tags=dm ./...`. Report skipped suites explicitly. CI has no enforced coverage percentage; maintain meaningful coverage of changed behavior.

## Commit & Pull Request Guidelines

History uses prefixes such as `fix:`, `docs:`, and `chore:` with descriptive Chinese summaries. Include a structured body explaining changes, reasons, and validation. PR descriptions should identify the problem, resulting behavior, related issues when applicable, and executed checks, including database coverage and skips. Update examples or documentation for API changes.

## Security & Configuration

Use disposable databases: integration tests migrate and clear tables. Configure credentials through `TEST_MYSQL_DSN`, `TEST_PG_DSN`, `TEST_ORACLE_DSN`, or `TEST_DM_DSN`; never commit real credentials or use production databases for tests.
