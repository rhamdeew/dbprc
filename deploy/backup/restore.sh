#!/bin/sh
# On the host: restore.sh <snapshot-id|latest>, alongside compose.yml and .env.
# Inside the backup image: restore.sh --fetch <snapshot> then --apply.
set -eu
umask 077

case "${1:-}" in
  --fetch)
    [ "$#" -eq 2 ] || exit 2
    if ! restic restore "$2" --target /restore >/dev/null 2>&1; then
      echo 'Snapshot download failed; check snapshot ID and repository access.' >&2
      exit 1
    fi
    test -f /restore/tmp/arc.db
    test -d /restore/uploads
    [ "$(sqlite3 /restore/tmp/arc.db 'PRAGMA integrity_check;')" = ok ]
    exit 0
    ;;
  --apply)
    test -f /restore/tmp/arc.db
    test -d /restore/uploads
    # API is stopped: discard stale WAL/SHM and replace uploads, including dotfiles.
    rm -f /data/arc.db /data/arc.db-wal /data/arc.db-shm
    cp /restore/tmp/arc.db /data/arc.db
    # The API image runs as arc (10001), while VACUUM/restic run as root.
    chown 10001:10001 /data/arc.db
    find /uploads -mindepth 1 -maxdepth 1 -exec rm -rf {} \;
    cp -a /restore/uploads/. /uploads/
    chown -R 10001:10001 /uploads
    exit 0
    ;;
esac

if [ "$#" -ne 1 ] || [ -z "$1" ]; then
  echo 'Usage: restore.sh <snapshot-id|latest>' >&2
  exit 2
fi
cd "$(dirname "$0")"
# An anonymous staging volume preserves the validated files across the two runs.
volume=$(docker volume create)
override=$(mktemp)
cleanup() {
  rm -f "$override"
  docker volume rm "$volume" >/dev/null
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
cat > "$override" <<'YAML'
services:
  backup:
    volumes:
      - data:/data:rw
      - uploads:/uploads:rw
YAML
compose() { docker compose -f compose.yml -f "$override" "$@"; }
# Download and validate before taking the app offline. A failed apply leaves it stopped.
compose run --rm -T -v "$volume:/restore" backup restore.sh --fetch "$1"
cron_running=$(compose ps --status running -q backup)
if [ -n "$cron_running" ]; then compose stop backup; fi
compose stop api
compose run --rm -T -v "$volume:/restore" backup restore.sh --apply
compose up -d api
if [ -n "$cron_running" ]; then compose up -d backup; fi
echo 'Restore complete. Check /healthz and the restored profiles/uploads.'
