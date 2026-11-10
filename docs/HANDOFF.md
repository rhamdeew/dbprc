# HANDOFF: state of the code for the next agent

Read this before exploring the code. **Update it in your PR** (see "How to update"). Keep it under ~120 lines: replace stale lines, don't append history. Facts only, with paths.

## Built so far (one line per card, newest last)

- Phase 0: walking skeleton: `/healthz` (reports version), `/api/version`, Caddy + Basic Auth, CI/CD, deploy with rollback.
- ARC-12 schema: all PoC tables in `backend/migrations/20261110211000_init.sql`; `internal/db` (WAL, write pool of 1 + read pool); sqlc → `internal/db/sqlc` (users only).
- ARC-18 uploads: `internal/upload` (`Store.Save/Delete`, kinds `Avatar|Photo|Drawing`, `upload.URL`).
- ARC-30 coverage gates: thresholds in `Makefile` (`GO_COVERAGE_MIN` 80, `FE_COVERAGE_MIN` 70); `scripts/coverage-gate.sh`.
- ARC-13 test harness: `internal/testutil` (DB, factories, `NewServer`, `LoginAs` stub); `cmd/mockoidc` fake Google on :9999; `make dev` runs mockoidc + api + vite.
- Tooling: `make` targets self-install pinned tools (`toolchain.env`, `scripts/toolchain.sh`).

## Code map

| Need | Where |
|---|---|
| Env config (only place that reads env) | `backend/internal/config/config.go`: `Env, Port, Version, DataDir, UploadDir`; `cfg.DBPath()` |
| Wiring | `backend/cmd/arc/main.go` opens DB, builds `server.New(cfg, log)` |
| Routes | `backend/internal/server/server.go` (`http.ServeMux`, Go 1.22 patterns) |
| DB access | `db.Open(ctx, path)` → `*db.DB` with `Writer()` / `Reader()`; queries via sqlc `backend/queries/<feature>.sql` → `make generate` |
| Migrations | `backend/migrations/` (goose, embedded, run on start). New file = new UTC timestamp |
| Images | `upload.New(dir)`; `Save(ctx, r, kind)` returns rel path; serve via `upload.URL(rel)` → `/uploads/...` (Caddy serves it) |
| Test DB / data | `testutil.NewDB(t)`; `CreateUser(t, db, WithEmail(..))`, `CreatePhoto`, `CreateWallPost`, `CreateChat`, `CreateMessage` (functional options) |
| HTTP tests | `testutil.NewServer(t, opts...)`, `testutil.LoginAs(t, srv, user)` |
| Fake Google | `backend/cmd/mockoidc`; issuer `http://localhost:9999/oidc`, client `arc-dev`/`arc-dev-secret`; `POST /_mock/next-user` queues the next identity |
| Frontend | `frontend/src`: `theme/theme.ts` (2006 palette), `test/render.tsx` (render with Mantine), `pages/` |

## Conventions established

- Tests: table-driven, real temp SQLite via `testutil.NewDB`, never mocks for the DB.
- New tables/columns: a new migration file, never edit `..._init.sql` (it already ran on staging/prod).
- sqlc: one query file per feature; commit the generated code (CI fails otherwise).
- Coverage excludes `internal/db/sqlc` and `cmd/`; keep logic out of `cmd/` so it is tested.
- Use `make` targets only; they pin tool versions.

## Gotchas / next steps already known

- `server.New(cfg, log)` gets **no DB or upload store yet**. The first API card (ARC-16) must change it to take dependencies (e.g. a `server.Deps` struct) and update `cmd/arc` + `testutil.NewServer`.
- `testutil.LoginAs` sends no session cookie yet: ARC-16 completes it (see its doc comment).
- The API does not read `OIDC_*` yet (ARC-15 adds them to `internal/config`).
- `/healthz` must keep returning `{"status":"ok","version":...}`: the deploy script checks the version.

## Open questions for the human

- (none)

## How to update (every card)

1. Add one line under "Built so far".
2. Change the code map, conventions or gotchas you made true or false.
3. Put anything a human must decide under "Open questions".
4. Remove lines that are no longer true. Stay under ~120 lines.
