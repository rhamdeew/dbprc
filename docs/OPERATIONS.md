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

## Backup and restore

Added by ARC-09.
