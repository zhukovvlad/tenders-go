# tenders-go — Workspace Instructions

Go REST API backend for a tender management system (government procurement / НМЦК).
Handles XLSX import → AI analysis → RAG matching → catalog deduplication.

---

## Module & Stack

```
Module:    github.com/zhukovvlad/tenders-go
Go:        1.24.3
Router:    github.com/gin-gonic/gin v1.10.1
DB:        database/sql + github.com/lib/pq + SQLC + pgvector
Auth:      JWT (httpOnly cookie) + CSRF (double-submit cookie)
Log:       cmd/pkg/logging (custom Logger interface wrapping logrus)
Config:    cleanenv (YAML: cmd/config/config.yml) + godotenv (.env)
```

---

## Essential Commands

```bash
# Run server (reads .env automatically)
make run

# Build & validate
go build ./...
go vet ./...

# DB setup (first time)
make setup-db           # starts Docker PG, creates DB, runs migrations

# DB migrations
make migrateup          # apply all
make migratedown1       # rollback last

# Regenerate SQLC + mocks (after changing *.sql query files)
make sqlc

# Tests
make test-unit          # fast, no DB (-short flag)
make test-integration   # real PG via Testcontainers (5 min timeout)
make test-e2e           # full workflow (10 min timeout)
make test-coverage      # generates coverage.html

# Utilities
make createadmin        # create first admin user via CLI
make generate-env       # generate secure GO_SERVER_API_KEY
```

**PostgreSQL:** `postgres://root:secret@localhost:5435/tendersdb?sslmode=disable`

---

## Architecture

```
HTTP Layer      cmd/internal/server/handlers_*.go
    ↓ (DTOs only, zero business logic)
Service Layer   cmd/internal/services/<domain>/
    ↓ (domain logic only, no HTTP knowledge)
Store Layer     cmd/internal/db/sqlc/  (SQLC-generated, read-only)
    ↓
PostgreSQL (pgvector, JSONB, FTS)
```

**Wiring:** `cmd/main/app.go` — constructor-based DI, no globals.

**Iron rules:**
1. Handlers parse, call service, map response — nothing else.
2. Services never import `gin` or reference `*gin.Context`.
3. Store is SQLC-only — no raw SQL outside `cmd/internal/db/query/*.sql`.
4. All cross-layer dependencies via interfaces (`db.Store`, `logging.Logger`).
5. Multi-table writes → `s.store.ExecTx(ctx, func(qtx *db.Queries) error {...})`.

---

## Project Layout

```
cmd/
  config/config.yml          # YAML config template
  createadmin/               # CLI tool for first admin
  internal/
    api_models/              # Shared API DTOs
    config/                  # Config struct (cleanenv)
    db/
      migration/             # 000001…000008 up/down SQL migrations
      query/                 # Source SQL files for SQLC
      sqlc/                  # SQLC-generated code + mock_store.go
    server/
      server.go              # Server struct, route registration
      handlers_*.go          # HTTP handlers by domain
      middleware_*.go        # auth, CSRF, rate-limit, service-auth
      converters.go          # DB → API response converters
    services/
      apierrors/             # ValidationError, NotFoundError, ConflictError
      auth/                  # JWT, bcrypt, refresh tokens
      catalog/               # Catalog positions, groups, merges
      entities/              # XLSX JSON ↔ DB struct transformers
      importer/              # Atomic tender import
      lot/                   # Lot CRUD, key parameters, AI integration
      matching/              # RAG position matching + cache
      settings/              # System settings KV store
    testutil/                # Test helpers: DB, fixtures, assertions, server
  main/app.go                # Entrypoint + DI wiring
  pkg/logging/               # Logger interface + logrus adapter
docs/devlog/                 # Architecture decision diary (YYYY-MM-DD_slug.md)
tests/
  integration/               # Integration tests (Testcontainers)
  e2e/                       # End-to-end tests
  fixtures/                  # JSON/SQL test data
```

---

## Services

| Service | Package | Role |
|---------|---------|------|
| Auth | `services/auth` | JWT lifecycle, bcrypt, refresh tokens |
| TenderImport | `services/importer` | Atomic XLSX → DB import |
| Catalog | `services/catalog` | Positions, groups, merges, FTS |
| Lot | `services/lot` | Lot CRUD, AI key-parameters |
| Matching | `services/matching` | RAG + cache-with-TTL |
| Settings | `services/settings` | System settings KV |
| Entities | `services/entities` | Domain model transformers |

---

## Route Security Model

| Surface | Auth | CSRF |
|---------|------|------|
| `GET /home`, `GET /api/stats` | None | No |
| `POST/GET /internal/worker/*` | `GO_SERVER_API_KEY` bearer | No |
| `POST /api/v1/auth/*` | None (login flow) | logout only |
| `GET /api/v1/*` | JWT cookie | No |
| `POST/PUT/PATCH/DELETE /api/v1/*` | JWT cookie | Yes |
| `/admin/*` | JWT cookie + `role=admin` | Yes |

---

## Naming Conventions

| Element | Pattern | Example |
|---------|---------|---------|
| Handler file | `handlers_<domain>.go` | `handlers_tender.go` |
| Handler method | `(s *Server) <action>Handler` | `listTendersHandler` |
| Service type | `<Domain>Service` | `CatalogService` |
| Constructor | `New<Type>` | `NewCatalogService` |
| Request DTO | `<action>Request` | `createTenderRequest` |
| Response DTO | `<entity>Response` | `LotResponse` |
| SQL query name | `<Action><Entity>` | `ListActiveTenders` |
| Migration file | `000XXX_<description>.{up,down}.sql` | |
| Devlog entry | `docs/devlog/YYYY-MM-DD_<slug>.md` | |

---

## Error Handling Pattern

**Creating errors (service layer only):**
```go
import "github.com/zhukovvlad/tenders-go/cmd/internal/services/apierrors"

apierrors.NewValidationError("field '%s' is required", field)  // → 400
apierrors.NewNotFoundError("tender %d not found", id)          // → 404
apierrors.NewConflictError("duplicate entry", conflicts)       // → 409
```

**Handling in handlers** — use `s.handleServiceError(c, err)` or switch:
```go
switch err.(type) {
case *apierrors.ValidationError: c.JSON(http.StatusBadRequest, errorResponse(err))
case *apierrors.NotFoundError:   c.JSON(http.StatusNotFound, errorResponse(err))
default:
    s.logger.WithError(err).Error("unexpected error")
    c.JSON(http.StatusInternalServerError, errorResponse(err))
}
```

Log errors **once**, at the HTTP boundary. Services wrap and propagate only.

---

## Implementing a New Feature (Checklist)

1. **Read** an analogous existing handler+service as a pattern before writing anything.
2. **Migration** (if schema changes) → `make migrateup`.
3. **SQL query** in `cmd/internal/db/query/<entity>.sql` → `make sqlc`.
4. **Service** in `cmd/internal/services/<domain>/<domain>_service.go`.
5. **Handler** in `cmd/internal/server/handlers_<domain>.go`.
6. **Register route** in `server.go` in the correct middleware group.
7. **Wire service** in `cmd/main/app.go` if new service.
8. `go build ./...` and `go vet ./...` must pass before commit.
9. **Devlog** in `docs/devlog/YYYY-MM-DD_<slug>.md` for non-trivial changes.

---

## SQL / SQLC Rules

- All DB queries live in `cmd/internal/db/query/*.sql`.
- Use `sqlc.arg(name)` for all parameters — no string concatenation in SQL.
- After any change to `*.sql` files: `make sqlc` to regenerate `cmd/internal/db/sqlc/`.
- SQLC annotations: `:one`, `:many`, `:exec`, `:execresult`.
- Nullable DB columns → `sql.NullString`, `sql.NullInt64`, `sql.NullTime`, `pqtype.NullRawMessage`.

---

## Testing Approach

- **Unit tests**: `make test-unit` — use generated mocks (`mock_store.go`, `mock_querier.go`), no Docker.
- **Integration tests**: `make test-integration` — real PostgreSQL via Testcontainers; prefer rollback cleanup.
- Use `cmd/internal/testutil/` helpers: `db_helper.go`, `fixtures.go`, `assertions.go`, `test_server.go`.
- Test naming: `TestDomain_Scenario_ExpectedOutcome` (e.g. `TestCreateTender_MissingTitle_ReturnsBadRequest`).
- BDD philosophy: test behavior, not implementation. Assume implementation is wrong until proven.

---

## Common Pitfalls

| Pitfall | Rule |
|---------|------|
| Business logic in handler | Move to service layer |
| `gin.Context` in service | Remove — pass only domain data |
| Raw SQL in Go code | Use SQLC query file + `make sqlc` |
| New DB column without migration | Migration first, then SQLC regenerate |
| `ExecTx` callback missing `return err` | Always return error to trigger rollback |
| Logging same error in service + handler | Log once at HTTP boundary only |
| `errors.New(...)` for user-facing errors | Use `apierrors.*` types |
| Duplicate route registration | Causes panic at startup — check existing routes |

---

## Python Workers (External)

The Go API integrates with Python FastAPI + Celery workers for:
- XLSX parsing (`/internal/worker/import-tender`)
- Gemini AI lot analysis (`/internal/worker/lots/:id/ai-results`)
- RAG position matching (`/internal/worker/positions/*`)
- Catalog indexing (`/internal/worker/catalog/*`)
- Merge suggestions (`/internal/worker/merges/suggest`)

Workers authenticate with `GO_SERVER_API_KEY` bearer token.
Missing Python workers → import and matching features fail silently (not a Go error).

---

## Skills (for deep detail)

| Task | Skill to invoke |
|------|----------------|
| New handler / service / migration / SQL | `coding-standards` |
| Writing unit or integration tests | `testing-go-app` |
| Modifying these instructions or creating agents | `agent-customization` |
