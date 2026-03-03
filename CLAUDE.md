# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Run all tests
go test ./...

# Run a specific test function
go test -run TestRepository_CRUD_And_Errors ./...

# Run a specific subtest
go test -run TestAdvanced_Features/SoftDelete_And_Unscoped ./...

# Run with verbose output (shows SQL logs via GORM logger)
go test -v ./...
```

**Known pre-existing test failures** (do not fix without explicit request):
- `TestBuilder_QuoteColumn/带别名(AS)` — `quoteColumn` exits early when it detects a space, so `"users.name AS u_name"` is not processed
- `TestBuilder_QuoteColumn/表名.*` — `*` is listed in the early-exit special characters

## Architecture

### Core Data Flow

```
User code
  → NewQuery[T](ctx) / NewUpdater[T](ctx)    (query.go / update.go)
      → getModelInstance[T]()                  (schema.go)  ← triggers registerModel on first call
  → q.Eq(&model.Field, val).Order(...)        (query.go / update.go)
  → repo.List(q) / repo.Update(u, tx)         (repository.go)
      → q.DataRuleBuilder().BuildQuery()       (query.go → builder.go)
          → ScopeBuilder.applyWhere/Joins/...  (builder.go)
      → GORM execution
```

### Two-Level Schema Cache (`schema.go` + `utils.go`)

This is the core mechanism that enables type-safe field pointers:

1. `utils.go / reflectStructSchema` — parses a struct type via reflection → `map[fieldOffset → columnName]`, cached by type string in `columnCache`
2. `schema.go / registerModel` — takes a concrete `*T` instance, walks `reflectStructSchema`, converts `baseAddress + offset → absoluteFieldAddress`, stores in `columnNameCache (sync.Map)`
3. `schema.go / getModelInstance[T]` — returns the **canonical cached pointer** for type `T`. The returned pointer is the exact instance whose base address was used in step 2. **Field pointers passed to query methods must come from this instance.**
4. `schema.go / resolveColumnName` — looks up an absolute field address in `columnNameCache`. Also accepts a plain `string` for raw column names.

**Critical invariant**: `NewQuery[T]` and `NewUpdater[T]` both return `(builder, *T)`. The `*T` is the registered singleton. All field address arguments (`&model.Name`) must come from that returned pointer — not from a separately created struct.

### `ScopeBuilder` (builder.go)

The shared base embedded in both `Query[T]` and `Updater[T]`. Holds conditions, selects, joins, orders, groups, havings, preloads, lock config. Exposes three build paths:

- `BuildQuery()` — full SELECT (select + distinct + where + joins + group/having + order/limit + lock + preloads)
- `BuildCount()` — COUNT path (where + joins + group/having only, no select/order/limit)
- `BuildUpdate()` — UPDATE path (where + joins + selects for column restriction)
- `BuildDelete()` — DELETE path (where only)

`quoteColumn` handles dialect-aware escaping: it detects `()+-*/, ` to skip complex expressions, and recursively handles `table.col` and `col AS alias` patterns.

### `DataRuleBuilder` (query.go)

Reads `[]DataRule` from `ctx.Value(DataRuleKey)` and appends conditions to the query. Protected by `dataRuleApplied bool` — calling it multiple times on the same `Query` is safe (idempotent). Always call it as `q.DataRuleBuilder().BuildQuery()`, never `q.BuildQuery()` directly in repository methods.

### Error Handling Pattern

Both `Query[T]` and `Updater[T]` accumulate errors in an `errs []error` slice during chain calls (e.g., if `resolveColumnName` fails). `GetError()` returns a joined error with a summary prefix (`"gplus query builder failed with N errors"` / `"gplus updater failed with N errors"`). Repository methods call `GetError()` early and return immediately on failure.

### Repository Error Variables

| Variable | Meaning |
|---|---|
| `ErrQueryNil` | nil Query/Updater passed |
| `ErrRawSQLEmpty` | empty string passed to `RawQuery`/`RawExec`/`RawScan` |
| `ErrDeleteEmpty` | `DeleteByCondTX` called with no conditions and not Unscoped |
| `ErrUpdateEmpty` | `Update` called with no fields in `setMap` |
| `ErrUpdateNoCondition` | `Update` called with fields but no WHERE conditions |
| `ErrTransactionReq` | `GetByLock` called without a transaction |

### Test Helpers (`model_test.go`)

Shared across all test files (same package `gplus`):
- `TestUser` struct with `BaseUser` embedding — used for schema/query tests
- `assertEqual(t, expected, actual, msg)` / `assertError(t, err, expectError, msg)` — standard assertion helpers
- `setupTestDB[T](t)` in `repo_test.go` — creates in-memory SQLite DB and auto-migrates `T`
- `setupAdvancedDB(t)` in `advanced_test.go` — creates DB with `UserWithDelete` + `Order` for join/preload/soft-delete tests

All tests run in-package (`package gplus`), giving access to unexported symbols like `quoteColumn` and `resolveColumnName`.
