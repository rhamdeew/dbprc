# ARC architecture

## Product rules (decided)

| Topic | Rule |
|---|---|
| Identity | Sign in with Google only. Accept ID tokens with `hd == ALLOWED_DOMAIN` and `email_verified == true` (checked server-side from the token claims). Account is created on first login. Name + avatar come from Google; the name is not editable. No passwords. |
| Gate | Basic Auth in front of everything (per environment) + company Google domain. `/healthz` is exempt from Basic Auth. |
| Profile | Avatar, interests (hobby, music, movies, other), up to 5 extra photos shown in a lightbox. |
| Wall | Anyone can post on anyone's wall: text and/or a drawing. Replies are 2 levels max (a reply to a reply attaches to the root). The wall owner can delete any post on their wall. |
| Anonymous posts | Author is stored. It is visible **only to the wall owner**; everyone else sees "Anonymous". The poster is warned about this in the UI. |
| Search | By email or name prefix, from the 4th character, max 10 results. |
| Random chat | "Start random chat" pairs you with a random available colleague. Prefer people you never chatted with; if none, re-match an existing partner: reuse that chat and add a system message "Matched again ☕". One chat per pair, kept forever. No way to pick a person. |
| Availability | Toggle "available for random chats". Unavailable: you are not matched and cannot start a random chat; existing chats keep working. |
| Notifications | In-app unread badges, unread count in the tab title, optional browser notifications. |
| Admin | Emails in `ADMIN_EMAILS`. `/admin` shows aggregate stats only and can hard-delete a profile by exact email (including their wall posts, authored posts and whole chats). No browsing, no impersonation. Non-admins get 404. Deleted users can sign in again and get an empty profile. |

## Runtime topology

```
Internet :80/:443
   │
edge Caddy (root, /opt/arc-edge on the VPS; TLS via Let's Encrypt)
   ├─ arc.dbdemo.live   → 127.0.0.1:8081   prod     (Linux user www,     ~/arc)
   └─ starc.dbdemo.live → 127.0.0.1:8082   staging  (Linux user staging, ~/arc)

per environment (deploy/env/compose.yml, project arc-<env>)
   web : Caddy + built frontend; Basic Auth; serves /uploads; proxies /api /auth /ws /healthz to api
   api : Go binary, SQLite in volume `data`, files in volume `uploads`
```

- One SQLite file per environment (WAL mode). Single api instance; the WebSocket hub is in memory.
- Images: `ghcr.io/rhamdeew/dbprc-{api,web}:<git sha>`. `/healthz` returns the running version.

## Branches and environments

| Branch | Role | Deploys to |
|---|---|---|
| `main` | Integration. Task PRs merge here when CI is green. | nothing |
| `staging` | Batch PR `main → staging`, merged by a human. | starc.dbdemo.live |
| `prod` | PR `staging → prod`, merged by a human (deploy also needs approval). | arc.dbdemo.live |

The deploy workflow builds images, renders `.env` from the GitHub Environment secrets, uploads `compose.yml`, and runs `deploy/env/deploy.sh` on the VPS. That script pulls first, switches, waits until `/healthz` reports the new version, and otherwise rolls back to the previous version.

## Configuration

All env vars are read in `backend/internal/config`. The list is in `AGENTS.md`. The API opens `$DATA_DIR/arc.db` (default directory `./data`) in WAL mode with foreign keys on. `deploy/env/compose.yml` already sets `DATA_DIR=/data`. Images are stored under `$UPLOAD_DIR` (default `./uploads`; compose sets `/uploads`) by `internal/upload` and served by the web container, not by the API.
