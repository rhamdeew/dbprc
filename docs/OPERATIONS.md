# Operations

## Deploy

- **staging:** merge a PR into `staging`. It deploys automatically in about 5 minutes.
- **prod:** merge a PR `staging → prod`, then approve the deployment in GitHub Actions (the `prod` environment requires approval).

Each deploy: CI → build images → render `.env` → `deploy/env/deploy.sh <sha>` on the VPS → smoke test through the edge (`/healthz` reports the sha, `/` asks for Basic Auth).

## Check what is running

```sh
curl -s https://starc.dbdemo.live/healthz      # {"status":"ok","version":"<sha>"}
ssh staging@ddr1 'cd ~/arc && cat .current_tag && docker compose ps'
ssh staging@ddr1 'cd ~/arc && docker compose logs --tail 100 api'
```

(prod: user `www`)

## Roll back by hand

`deploy.sh` rolls back automatically when the new version fails its health check. To go back to an older version on purpose: re-run the "Deploy" workflow for that commit (Actions → Deploy → the old run → Re-run all jobs). Alternatively, revert the commit on the branch.

## Server

| What | Where |
|---|---|
| Provisioning (idempotent) | `deploy/provision.sh`, run as root |
| Edge Caddy | `/opt/arc-edge` (`docker compose -f /opt/arc-edge/compose.yml logs`) |
| Env stacks | `~/arc` of user `www` (prod) and `staging` |
| GitHub secrets + deploy keys | `deploy/setup-github.sh staging\|prod` (re-run to rotate) |
| Firewall | ufw: SSH port, 80, 443 |

## Chat message keys

Before deploying ARC-37, generate a different key for each environment with `openssl rand -base64 32`. Store each key in the password manager with its environment and creation date, and set the corresponding GitHub Environment secret `MESSAGE_KEY`. Never use the public key from `.env.dev.example` outside local development/tests. Deploy renders the required key into the protected `.env`; Compose passes it to API. Optional `MESSAGE_KEY_OLD` is rendered only when set. Missing or malformed keys prevent API startup. Never log keys or include them in database backups. **Losing the key loses all messages encrypted with it.** Password-manager recovery must be independent of the VPS and restic repository.

To rotate once:

1. Preserve the current key in the password manager and set it as the environment's `MESSAGE_KEY_OLD` secret.
2. Generate and save a new 32-byte key; set it as `MESSAGE_KEY` in the same environment before deploying. Deploy both changes together.
3. Verify an old chat and send a new message. Reads try current then old; new writes always use current. A missing key or damaged row displays `[message unavailable]` and logs the row id.
4. Keep the old key configured while live rows still use it. This release does not re-encrypt historical rows, so elapsed time does not make removal safe. Only one previous key can be configured: before another rotation, re-encrypt older live rows through a separately reviewed maintenance procedure. Retain all historical keys securely for any backups that still depend on them. Do not remove a key merely because new messages work.

Restoring a database also requires its message keys from the password manager; restic recovery alone is insufficient. An older API image predating ARC-37 cannot read encrypted chat bodies, so avoid rolling back to it after encrypted writes.

## Backups

The backup container runs at **03:00 UTC in both staging and prod**. Restic encrypts the SQLite snapshot and uploads. Retention: 7 daily, 4 weekly, 6 monthly. The DB snapshot uses `VACUUM INTO`; uploads are read live, so a concurrent upload/deletion can straddle the DB snapshot. Restic errors fail the job. Prod also backs up after pulling new images and before switching; failure aborts the deploy without changing the active config or containers. An initialized DB is required for that backup (bootstrap a new environment without backup configured, then enable it).

Set these GitHub Environment secrets separately for staging and prod (never commit them):

| Secret | Meaning |
|---|---|
| `RESTIC_REPOSITORY` | `s3:https://<account>.r2.cloudflarestorage.com/<bucket>`; existing buckets: `st-arc` staging, `arc` prod |
| `RESTIC_PASSWORD` | Restic encryption password; keep a separate secure copy, loss prevents recovery |
| `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` | Per-bucket API token; rendered as `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` |
| `BACKUP_PING_URL` | Optional monitoring URL; success pings it, failure appends `/fail`; empty means no ping |

Deploy renders `COMPOSE_PROFILES=backup` only when `RESTIC_REPOSITORY` is nonempty. Empty repository skips both cron and prod pre-deploy backup. The prod pre-deploy backup is also skipped on the first release, when the data volume is readable and has no `arc.db` yet (the deploy log says so); any other backup failure stops the deploy with nothing changed. The image is published with the same SHA as API/web. Never run scripts with shell tracing or print `.env`/Compose config containing secrets.

From the environment user's `~/arc`:

```sh
docker compose run --rm -T backup backup.sh
docker compose run --rm -T backup restic snapshots
docker compose logs --tail 100 backup
```

## Restore

Use `www` for prod or `staging` for staging. Restoring replaces the entire DB and uploads, including deleting files absent from the snapshot. Take a fresh backup first when possible, record the desired snapshot ID, and schedule downtime. No schema migration rollback is performed: use an API version compatible with the snapshot.

```sh
cd ~/arc
sh ./restore.sh <snapshot-id>    # or: sh ./restore.sh latest
curl -fsS http://127.0.0.1:8081/healthz  # staging: 8082
docker compose logs --tail 50 api
```

The command downloads into a temporary Docker volume and checks SQLite integrity before stopping API. It pauses cron if running, then mounts data/uploads read-write, replaces the DB, removes stale WAL/SHM, replaces uploads, restores API ownership (uid 10001), and starts API and resumes previously running cron. A download/validation failure leaves API running. An apply failure leaves API and cron stopped for inspection; fix the cause and rerun. Verify profiles, photos, wall drawings and chats after restore. Restored sessions are included in the DB. Temporary restore volumes are removed on exit.

## Restore into staging

1. Use the staging Linux user and `~/arc`, ensuring the Compose project is `arc-staging`. Never run this procedure from the prod directory/user.
2. Stop staging's cron container: `docker compose stop backup`. Securely copy staging `.env` to `.env.before-restore` (mode 600).
3. Temporarily edit **staging's** `.env` with a secure editor: set `RESTIC_REPOSITORY`, `RESTIC_PASSWORD`, `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` to the source prod repository credentials. Keep `COMPOSE_PROJECT_NAME`, `APP_ENV`, ports, and other API settings unchanged. To read restored prod chats, temporarily set `MESSAGE_KEY` and, if needed, `MESSAGE_KEY_OLD` to the source snapshot’s message keys from the password manager. These are sensitive prod credentials: restrict staging access and do not send new chats during inspection. Set `BACKUP_PING_URL` empty. Do not start the cron container with source credentials; only run restore/read-only restic commands.
4. Run `sh ./restore.sh <prod-snapshot-id>`; check staging on port 8082 and `https://starc.dbdemo.live`. Prod volumes are never mounted by this project. Staging now contains prod profiles and sessions; keep access restricted.
5. Restore `.env.before-restore` to `.env`, then `docker compose up -d api backup` to recreate API with staging’s own keys and resume staging backups into `st-arc`. Remove the temporary credential copy securely. Restored prod chats become unreadable under staging’s distinct keys; for a lasting staging dataset, use a separately reviewed re-encryption procedure. Verify staging settings before the next deploy.

`make test-backup` builds the backup image and tests encrypted local-repository initialization, repeated backup/retention, bad password failure, wipe/restore, byte-identical DB and upload comparison, stale-file cleanup, and failed snapshot lookup without stopping API. `make lint-backup` runs shellcheck. Both run in CI without R2 access.
