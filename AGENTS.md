# AGENTS.md: rules for AI agents working on ARC

ARC (Async Random Coffee) is an internal web app for about 300 colleagues: profiles with a wall, colleague search, and 1-to-1 chats with a **random** teammate. Read `docs/ARCHITECTURE.md` before your first change.

You get work as a board card (ARC-XX). Do **exactly** that card. If the card conflicts with this file, follow the card and mention it in the PR.

## Stack (do not add alternatives)

- **Backend:** Go (see `backend/go.mod`), stdlib `net/http` routing (`mux.HandleFunc("GET /path/{id}", …)`), `log/slog`, SQLite via `modernc.org/sqlite` (no CGO), goose migrations, sqlc, `alexedwards/scs` sessions, `coreos/go-oidc`, `coder/websocket`.
- **Frontend:** React + TypeScript + Vite, **Mantine** (the only UI kit), TanStack Query (server state), React Router. No Tailwind, no other component libraries, no CSS-in-JS libs.
- **Tests:** Go `testing` (table-driven, `httptest`, real temp SQLite), Vitest + React Testing Library + MSW, Playwright for E2E.

## Layout

```
backend/cmd/arc/            main: wiring only
backend/cmd/mockoidc/       fake Google for `make dev` and E2E, 127.0.0.1:9999
backend/internal/config/    env config: every env var is read here and nowhere else
backend/internal/server/    root router; each feature registers its routes here with one line
backend/internal/<feature>/ handlers + service + tests for one feature (auth, profile, wall, chat, …)
backend/migrations/         goose SQL, named <UTC timestamp>_<name>.sql (never sequential numbers)
backend/queries/            sqlc: one <feature>.sql per feature
frontend/src/api/           API client + typed endpoints
frontend/src/pages/         one folder or file per route
frontend/src/components/    shared components
frontend/src/theme/         Mantine theme ("2006 social network" look)
e2e/                        Playwright
deploy/, .github/workflows/ infrastructure: do not modify unless the card says so
```

## Commands

```
make toolchain  # FIRST in a fresh container: pinned Go, golangci-lint, sqlc + deps into ~/.local (~1 min)
make setup      # frontend deps only
make lint    # golangci-lint + eslint + tsc
make test    # all unit/integration tests, no Docker or network needed
make dev     # mockoidc :9999 + api :8080 + vite :5173
```

`make lint test` must pass before you push. Always use the make targets: they put the pinned tools (`toolchain.env`, Go from `backend/go.mod`) first on PATH. Do not install Go, golangci-lint or sqlc any other way, and never change their versions unless the card says so.

## Conventions

- **API errors:** JSON `{"code":"snake_case","message":"human text"}` with the right status. Unknown/forbidden resources of other users → 404, not 403.
- **Auth:** every `/api/*` route except explicitly public ones requires a session. Never trust IDs from the client for ownership; use the session user.
- **SQL:** parameterized only (sqlc). Every multi-step write runs in one transaction.
- **Time:** UTC everywhere, RFC3339 in JSON.
- **Files:** user uploads only through `internal/upload`. Never serve user files from the API.
- **Logging:** `slog` with key/value pairs. Never log secrets, session tokens, message bodies or emails in clear text.
- **No global mutable state.** Pass dependencies explicitly.
- **Frontend:** data fetching only through TanStack Query hooks in `src/api`. Components stay small; pages compose them. Accessible labels on every input and button.
- **Copy/text:** English, short, friendly.

## Tests and definition of done

A card is done when:
1. The behavior described in the card works.
2. Tests cover it: every endpoint (happy path + each error branch in the card), every non-trivial function, every UI state the card mentions.
3. `make lint test` passes locally and CI is green.
4. No unrelated changes, no TODOs without a card reference.

Coverage targets: Go ≥ 80%, frontend ≥ 70% (lines).

## Git and PRs

- Branch: `task/ARC-XX-short-slug` from the latest `origin/main`.
- Commits: `ARC-XX: <imperative summary>`.
- PR into `main`, title `ARC-XX: <card title>`. Body: what changed, how it was tested, open questions.
- Never push directly to `main`, `staging` or `prod`. Never merge into `staging` or `prod`: humans do that.
- Never commit secrets, `.env` files, databases or build output.

## Environment variables

Read only in `internal/config`. Current and planned: `APP_ENV` (dev|test|staging|prod), `PORT`, `APP_BASE_URL`, `DATA_DIR` (default `./data`; SQLite file `$DATA_DIR/arc.db`), `UPLOAD_DIR` (default `./uploads`), `SESSION_SECRET`, `SESSION_COOKIE`, `OIDC_ISSUER`, `OIDC_CLIENT_ID`, `OIDC_CLIENT_SECRET`, `ALLOWED_DOMAIN`, `ADMIN_EMAILS`, `STATIC_DIR` (dev/E2E only). Adding a new variable: document it here and in `docs/ARCHITECTURE.md`, and say in the PR body that `.github/workflows/deploy.yml` needs it (a human wires the secret).

## When you are stuck

If the card is ambiguous, contradicts the code, or needs a human decision: stop, comment on the card with the exact question and your recommended answer, and move it to **Blocked**. Do not guess on product decisions.
