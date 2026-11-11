# HANDOFF: state of the code for the next agent

Read this before exploring the code. **Update it in your PR** (see "How to update"). Keep it under ~120 lines: replace stale lines, don't append history. Facts only, with paths.

## Built so far (one line per card, newest last)

- Phase 0: walking skeleton: `/healthz` (reports version), `/api/version`, Caddy + Basic Auth, CI/CD, deploy with rollback.
- ARC-12 schema: all PoC tables in `backend/migrations/20261110211000_init.sql`; `internal/db` (WAL, write pool of 1 + read pool); sqlc → `internal/db/sqlc`.
- ARC-18 uploads: `internal/upload` (`Store.Save/Delete`, kinds `Avatar|Photo|Drawing`, `upload.URL`).
- ARC-30 coverage gates: thresholds in `Makefile` (`GO_COVERAGE_MIN` 80, `FE_COVERAGE_MIN` 70); `scripts/coverage-gate.sh`.
- ARC-13 test harness: `internal/testutil` (DB, factories, `NewServer`, `LoginAs`); `cmd/mockoidc` fake Google on :9999; `make dev` runs mockoidc + api + vite.
- Tooling: `make` targets self-install pinned tools (`toolchain.env`, `scripts/toolchain.sh`).
- ARC-14 frontend shell: Mantine theme, routes, `src/api/client.ts` (`api()`), `useMe` calls `GET /api/me`.
- ARC-16 sessions: `internal/session` (scs + sqlite3store), `httpx.RequireUser` + CSRF, `GET /api/me`, `POST /auth/logout`, `LoginAs` mints a real session.
- ARC-15 Google sign-in: `GET /auth/google/start` and `GET /auth/google/callback` (PKCE, domain + verified email from the ID token, signed `arc_oidc` cookie).
- ARC-19 profile API: `GET /api/users/{id}`, `PUT /api/me/interests`, `POST /api/me/avatar`, `POST /api/me/photos` (max 5), `DELETE /api/me/photos/{id}`.
- ARC-21 wall API: `GET`/`POST /api/users/{id}/wall`, `DELETE /api/wall/{postId}`. Replies flatten to the root. Anonymous `author` is filled only for the wall owner.
- ARC-22 drawing pad: `frontend/src/components/DrawingPad` (`strokes.ts` + `perfect-freehand`; PNG blob per finished stroke).
- ARC-25 random chat API: availability, `POST /api/chats/random` (fresh partner, else re-match), chat list, messages, monotonic read cursor. `chat.Notifier` is a no-op until ARC-26.
- ARC-17 login page: `/login` wordmark, tagline, Google link with `next`, `?error=` copy; top-bar logout posts `/auth/logout` and clears the query cache.

## Code map

| Need | Where |
|---|---|
| Env config (only place that reads env) | `internal/config`: `AppBaseURL`, `SessionCookie` (default `arc_sid`), `AdminEmails`, `OIDCIssuer` (default `https://accounts.google.com`), `OIDCClientID`, `OIDCClientSecret`, `AllowedDomain`; `cfg.IsAdmin`, `config.Origin` |
| Wiring | `cmd/arc` opens DB, `server.New(cfg, server.Deps{DB, Log})`. `Server.Close` stops session cleanup before the DB closes |
| Routes | `internal/server`: public `GET /healthz`, `GET /api/version`, `GET /auth/google/*`. `auth.Routes`: `GET /api/me`, `POST /auth/logout`, Google. `profile.Routes`: `GET /api/users/{id}`, `PUT /api/me/interests`, `POST /api/me/avatar`, `POST /api/me/photos`, `DELETE /api/me/photos/{id}`. `wall.Routes`: `GET` and `POST /api/users/{id}/wall`, `DELETE /api/wall/{postId}` |
| Middleware | `sessions.LoadAndSave`, then `httpx.CSRF(APP_BASE_URL)`, then `logRequests`. `httpx.RequireUser` on `/api/me`, profile, wall, and chat routes puts `sqlc.User` in the context (`httpx.UserFrom`) |
| Chat | `internal/chat` from `server.New` (`Deps.ChatNotifier`, nil = `chat.NopNotifier`). `PUT /api/me/availability`, `POST /api/chats/random`, `GET /api/chats`, `GET`/`POST /api/chats/{id}/messages`, `POST /api/chats/{id}/read`. Queries in `queries/chat.sql` |
| Sessions | `internal/session` on `db.Writer()`. Cookie `arc_sid`, HttpOnly, Secure when `APP_ENV!=dev`, SameSite=Lax. Absolute lifetime 30d, idle 7d (cookie Max-Age follows the idle deadline and slides on each request) |
| DB access | `db.Open` → `Writer()` / `Reader()`; sqlc `backend/queries/<feature>.sql` → `make generate` |
| Migrations | `backend/migrations/` (goose, embedded). `sessions` table already matches sqlite3store. New file = new UTC timestamp |
| Images | `upload.URL(rel)` → `/uploads/...`. `avatarUrl` uses it, or null. First Google login saves `picture` via `upload.Store` (failure leaves avatar null). `POST /api/me/avatar` replaces the path and deletes the previous file (a missing file is fine). A rejected 6th photo and `DELETE /api/me/photos/{id}` remove the file |
| Test DB / HTTP | `testutil.NewDB`, `NewServer` (sets `AppBaseURL` to the server URL when empty), `Database(t, srv)`, `LoginAs` (Origin = server URL, session cookie when the handler is `*server.Server`), `SessionCookie` |
| Frontend | `frontend/src`: `theme/theme.ts`, `api/client.ts`, `api/me.ts` (`Me` shape), `api/auth.ts` (`useLogout`), `pages/LoginPage.tsx` (outside `RequireAuth`), `app/routes.tsx`, `components/DrawingPad` (`onChange` PNG blob; not wired to the wall yet) |

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
- `sqlite3store` uses `$1` and `julianday` on `*sql.DB`. It works with `modernc.org/sqlite`. Do not add `mattn/go-sqlite3`. Call `Server.Close` (or `session.Manager.Close`) once or the cleanup goroutine leaks; a second `StopCleanup` from the library would block. Do not write the HTTP response while a `Writer()` transaction is open: `WriteHeader` commits the session and needs that single connection.
- A user deleted from `users` is 401 and the session row is removed. An expired `sessions.expiry` is 401.
- `SESSION_SECRET` is still unused (scs tokens are random). The OIDC state cookie `arc_oidc` is HMAC-signed with a process-local key (a restart drops in-flight logins), HttpOnly, SameSite=Lax, Path `/auth/google`, 10 min, Secure unless `APP_ENV=dev`. Bad nonce and bad state both redirect to `/login?error=state`. `hd` is taken from the ID token, not the authorize URL. `next` must be a path starting with `/` and not `//`.
- `.env.dev.example` sets `APP_BASE_URL=http://localhost:5173` so the vite origin passes CSRF, and points `OIDC_*` / `ALLOWED_DOMAIN` at mockoidc. `OIDC_ISSUER` defaults to Google, so deploy.yml does not set it.
- `/healthz` must keep returning `{"status":"ok","version":...}`.
- Profile: interests are trimmed, then limited to 500 characters (Unicode code points). PUT replaces all four fields; an omitted field is stored empty. Name and email change only on Google sign-in. The photo cap is `COUNT` + `INSERT` in one write transaction; the 6th returns 409 `photo_limit` and the new file is discarded. Another user's photo is 404 `not_found`. Other codes: 400 `too_long` / `invalid` / `unsupported_type` / `invalid_image` / `too_large`.
- Wall: `GET` returns `{posts}` (roots newest first by `created_at`, then `id`). `limit` defaults to 20 and must be 1–100. `before` is a root post id on that wall (exclusive); a reply or another wall is 404. Each root includes every reply, oldest first. `POST` is multipart: `body` trimmed to ≤ 2000 runes and/or a PNG `drawing`, `anonymous` (`true`/`false`/`1`/`0`/`yes`/`no`/`on`/`off`), optional `parentId`. A reply's `parent_id` is always the root. `author` is null on anonymous posts unless the viewer owns the wall (`isAnonymous` stays true). Responses never include `author_id`. `canDelete` is true only for the wall owner. `DELETE` is 404 for everyone else; a root delete cascades replies and removes drawing files after commit.
- Drawing pad: 400×200 CSS pixels, bitmap × `devicePixelRatio`, white `#ffffff`, `touch-action: none`. Colors `#000` `#3b5998` `#d93025` `#188038`. Brushes 4 and 12 (card did not specify diameters). `onChange` runs after pointerup, pointercancel, undo, and clear (`null` when no finished strokes), not on mount or mid-stroke. Mouse pressure 0.5 is simulated; other pressure is kept. Undo drops the last finished stroke.
- Login (`pages/LoginPage.tsx`, `pages/login.ts`): wordmark "ARC", tagline "Async Random Coffee: meet your teammates when you have time", one link "Sign in with Google". `error=domain` → "Use your company Google account"; `unverified` → "Your Google email is not verified."; `state` → "Sign-in expired. Try again."; `provider` → "Google sign-in is unavailable. Try again."; any other code → "Sign-in failed. Try again." `next` is copied onto `/auth/google/start` (omitted when empty; not checked here, the API still rejects a non-path). Logout (`api/auth.ts`) is `POST /auth/logout`, then `queryClient.clear()`, then `/login` with no `next`. A failed logout stays on the page and shows the API error.
- Chat: `users.available` defaults to on. `PUT /api/me/availability` requires `{"available":bool}` and returns that flag. `POST /api/chats/random` is 409 `you_are_unavailable` or 404 `nobody_available`, else 201 `{chat, rematch}`. One `BEGIN IMMEDIATE` on the writer picks a random available user with no chat, else a random available existing partner (unique pair is retried once). New rows store `user_low < user_high` and no opening message. Re-match inserts system `Matched again ☕` (`sender_id` NULL, kind `system`) and bumps `last_message_at`. `GET /api/chats` is a JSON array ordered by `last_message_at` desc, then id. `lastMessage` is null when there are no messages and is `{body, kind, createdAt}`. `unreadCount` skips the caller's own rows; system lines count. `GET .../messages` is `{messages}` oldest-first inside the page (latest page when `before` is omitted). `limit` defaults to 50 and must be 1–100. `before` is an exclusive message id in that chat. `POST .../messages` trims to 1–4000 runes (`invalid` / `too_long`) and works while either member is unavailable. `POST .../read` `{lastMessageId}` never moves backwards and returns the stored id. Non-members and unknown chats are 404 `not_found` (a bad cursor in a chat you belong to is 404 `Message not found.`). `ChatCreated` fires for a new pair and `MessageCreated` for a text or system line, after commit, with both member ids. Message bodies are not logged.

## Open questions for the human

- (none)

## How to update (every card)

1. Add one line under "Built so far".
2. Change the code map, conventions or gotchas you made true or false.
3. Put anything a human must decide under "Open questions".
4. Remove lines that are no longer true. Stay under ~120 lines.
