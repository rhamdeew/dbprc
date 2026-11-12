#!/usr/bin/env bash
# Runs on the VPS as the env user, from ~/arc. Called by .github/workflows/deploy.yml.
# Usage: deploy.sh <new-tag>
# Expects ./compose.yml and ./.env.new (written by the workflow, without TAG).
# Success = /healthz answers with the new version. Otherwise rolls back to the previous tag.
set -euo pipefail
cd "$(dirname "$0")"

NEW_TAG="$1"
PREV_TAG="$(cat .current_tag 2>/dev/null || true)"

with_tag() { # <env-file> <tag> → rewrites TAG in place
  grep -v '^TAG=' "$1" > "$1.tmp" || true
  echo "TAG='$2'" >> "$1.tmp"
  mv "$1.tmp" "$1"
  chmod 600 "$1"
}
compose() { docker compose --env-file "$1" -f compose.yml "${@:2}"; }

with_tag .env.new "$NEW_TAG"

# 1. Pull first: if images are missing, nothing running is touched.
if ! compose .env.new pull --quiet; then
  echo "pull failed for $NEW_TAG, nothing changed" >&2
  rm -f .env.new
  exit 1
fi

port="$(grep '^WEB_PORT=' .env.new | cut -d= -f2 | tr -d "'")"
running_version() { curl -fsS "http://127.0.0.1:${port}/healthz" 2>/dev/null || true; }
wait_for() { # <tag>
  for _ in $(seq 1 45); do
    [[ "$(running_version)" == *"\"version\":\"$1\""* ]] && return 0
    sleep 2
  done
  return 1
}

# Prod takes a fresh snapshot before any running service or active config is changed.
# --env-file supplies the incoming credentials; backup has no env_file dependency.
if grep -qx "APP_ENV='prod'" .env.new && grep -qx "COMPOSE_PROFILES='backup'" .env.new; then
  if ! compose .env.new run --rm -T backup backup.sh; then
    echo "pre-deploy backup failed, nothing changed" >&2
    rm -f .env.new
    exit 1
  fi
fi

# 2. Switch.
if [[ -f .env ]]; then cp .env .env.prev; fi
mv .env.new .env
compose .env up -d --remove-orphans || true

if wait_for "$NEW_TAG"; then
  echo "$NEW_TAG" > .current_tag
  docker image prune -af --filter "until=168h" >/dev/null 2>&1 || true
  echo "deployed $NEW_TAG"
  exit 0
fi

# 3. Roll back.
echo "health check failed for $NEW_TAG; healthz said: $(running_version)" >&2
compose .env logs --tail 50 >&2 || true
if [[ -n "$PREV_TAG" && -f .env.prev ]]; then
  echo "rolling back to $PREV_TAG" >&2
  mv .env.prev .env
  compose .env up -d --remove-orphans || true
  if wait_for "$PREV_TAG"; then echo "rollback to $PREV_TAG healthy" >&2; else echo "ROLLBACK UNHEALTHY" >&2; fi
fi
exit 1
