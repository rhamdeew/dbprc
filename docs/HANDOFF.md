# HANDOFF: state of the code for the next agent

Read this before exploring the code. **Update it in your PR** (see "How to update"). Keep it under ~120 lines: replace stale lines, don't append history. Facts only, with paths.

## Built so far (one line per card, newest last)

- Phase 0: walking skeleton: `/healthz` (reports version), `/api/version`, Caddy + Basic Auth, CI/CD, deploy with rollback.
- ARC-12 schema: all PoC tables in `backend/migrations/20261110211000_init.sql`; `internal/db` (WAL, write pool of 1 + read pool); sqlc → `internal/db/sqlc` (users only).
- ARC-18 uploads: `internal/upload` (`Store.Save/Delete`, kinds `Avatar|Photo|Drawing`, `upload.URL`).
- ARC-30 coverage gates: thresholds in `Makefile` (`GO_COVERAGE_MIN` 80, `FE_COVERAGE_MIN` 70); `scripts/coverage-gate.sh`.
- ARC-13 test harness: `internal/testutil` (DB, factories, `NewServer`, `LoginAs`); `cmd/mockoidc` fake Google on :9999; `make dev` runs mockoidc + api + vite.
- Tooling: `make` targets self-install pinned tools (`toolchain.env`, `scripts/toolchain.sh`).
- ARC-14 frontend shell: Mantine theme, routes, `src/api/client.ts` (`api()`), `useMe` calls `GET /api/me`.
- ARC-16 sessions: `internal/session` (scs + sqlite3store), `httpx.RequireUser` + CSRF, `GET /api/me`, `POST /auth/logout`, `LoginAs` mints a real session.
- ARC-15 Google sign-in: `GET /auth/google/start` and `GET /auth/google/callback` (PKCE, domain + verified email from the ID token, signed `arc_oidc` cookie).

## Code map

| Need | Where |
|---|---|
| Env config (only place that reads env) | `internal/config`: `AppBaseURL`, `SessionCookie` (default `arc_sid`), `AdminEmails`, `OIDCIssuer` (default `https://accounts.google.com`), `OIDCClientID`, `OIDCClientSecret`, `AllowedDomain`; `cfg.IsAdmin`, `config.Origin` |
| Wiring | `cmd/arc` opens DB, `server.New(cfg, server.Deps{DB, Log})`. `Server.Close` stops session cleanup before the DB closes |
| Routes | `internal/server`: public `GET /healthz`, `GET /api/version`, `GET /auth/google/start`, `GET /auth/google/callback`. `auth.Routes`: those plus `GET /api/me`, `POST /auth/logout` |
| Middleware | `sessions.LoadAndSave`, then `httpx.CSRF(APP_BASE_URL)`, then `logRequests`. `httpx.RequireUser` on `/api/me` puts `sqlc.User` in the context (`httpx.UserFrom`) |
| Sessions | `internal/session` on `db.Writer()`. Cookie `arc_sid`, HttpOnly, Secure when `APP_ENV!=dev`, SameSite=Lax. Absolute lifetime 30d, idle 7d (cookie Max-Age follows the idle deadline and slides on each request) |
| DB access | `db.Open` → `Writer()` / `Reader()`; sqlc `backend/queries/<feature>.sql` → `make generate` |
| Migrations | `backend/migrations/` (goose, embedded). `sessions` table already matches sqlite3store. New file = new UTC timestamp |
| Images | `upload.URL(rel)` → `/uploads/...`. `GET /api/me` `avatarUrl` uses it, or null. First Google login saves `picture` via `upload.Store` (failure leaves avatar null) |
| Test DB / HTTP | `testutil.NewDB`, `NewServer` (sets `AppBaseURL` to the server URL when empty), `Database(t, srv)`, `LoginAs` (Origin = server URL, session cookie when the handler is `*server.Server`), `SessionCookie` |
| Frontend | `frontend/src`: `theme/theme.ts`, `api/client.ts`, `api/me.ts` (`Me` shape), `app/routes.tsx` |

## Conventions established

- Tests: table-driven, real temp SQLite via `testutil.NewDB`, never mocks for the DB.
- New tables/columns: a new migration file, never edit `..._init.sql`.
- sqlc: one query file per feature; commit generated code.
- Coverage excludes `internal/db/sqlc` and `cmd/`.
- API errors: `{"code","message"}`. 401 `unauthenticated`, CSRF 403 `csrf`, unexpected 500 `internal`.
- Public routes are `/healthz`, `/api/version`, and `GET /auth/google/*`. New `/api` routes go behind `RequireUser`.
- Use `make` targets only; they pin tool versions.

## Gotchas / next steps already known

- CSRF allows a mutating request when `Origin` equals the origin of `APP_BASE_URL` (path on the base URL is ignored). If `Origin` is absent, only `Sec-Fetch-Site: same-origin` (exact, lowercase) is allowed. `LoginAs` overwrites `Origin` with the test server URL, so a test that sets `AppBaseURL` to something else must send `Origin` itself.
- `sqlite3store` uses `$1` and `julianday` on `*sql.DB`. It works with `modernc.org/sqlite`. Do not add `mattn/go-sqlite3`. Call `Server.Close` (or `session.Manager.Close`) once or the cleanup goroutine leaks; a second `StopCleanup` from the library would block.
- A user deleted from `users` is 401 and the session row is removed. An expired `sessions.expiry` is 401.
- `SESSION_SECRET` is still unused (scs tokens are random). The OIDC state cookie `arc_oidc` is HMAC-signed with a process-local key (a restart drops in-flight logins), HttpOnly, SameSite=Lax, Path `/auth/google`, 10 min, Secure unless `APP_ENV=dev`. Bad nonce and bad state both redirect to `/login?error=state`. `hd` is taken from the ID token, not the authorize URL. `next` must be a path starting with `/` and not `//`.
- `.env.dev.example` sets `APP_BASE_URL=http://localhost:5173` so the vite origin passes CSRF, and points `OIDC_*` / `ALLOWED_DOMAIN` at mockoidc. `OIDC_ISSUER` defaults to Google, so deploy.yml does not set it.
- `/healthz` must keep returning `{"status":"ok","version":...}`.

## Open questions for the human

- (none)

## How to update (every card)

1. Add one line under "Built so far".
2. Change the code map, conventions or gotchas you made true or false.
3. Put anything a human must decide under "Open questions".
4. Remove lines that are no longer true. Stay under ~120 lines.
