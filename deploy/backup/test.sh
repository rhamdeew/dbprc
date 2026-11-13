#!/bin/sh
# Isolated Docker volumes and a local encrypted repository: never touches a VPS or R2.
set -eu
cd "$(dirname "$0")/../.."
root=$(pwd)
work=$(mktemp -d)
project="arc-backup-test-$$"
compose() { docker compose -p "$project" -f "$work/compose.yml" "$@"; }
cleanup() {
  compose down -v --remove-orphans >/dev/null 2>&1 || true
  rm -rf "$work"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
docker build -t arc-backup-test:local -f deploy/backup/Dockerfile .
cp "$root/deploy/backup/restore.sh" "$work/restore.sh"
cat > "$work/compose.yml" <<'YAML'
services:
  api:
    image: arc-backup-test:local
    command: [sleep, infinity]
  writer:
    image: arc-backup-test:local
    command:
      - sh
      - -ec
      - '{ printf "PRAGMA journal_mode=WAL; SELECT count(*) FROM sample;\n"; sleep infinity; } | sqlite3 /data/arc.db'
    volumes:
      - data:/data
  backup:
    image: arc-backup-test:local
    environment:
      RESTIC_REPOSITORY: /repo
      RESTIC_PASSWORD: local-test-password
      BACKUP_PING_URL: ''
    volumes:
      - data:/data:ro
      - uploads:/uploads:ro
      - repo:/repo
      - expected:/expected
volumes:
  data:
  uploads:
  repo:
  expected:
YAML
cat > "$work/seed.yml" <<'YAML'
services:
  backup:
    volumes:
      - data:/data:rw
      - uploads:/uploads:rw
YAML
compose -f "$work/seed.yml" run --rm -T backup sh -ec '
  mkdir -p /uploads/photos /expected/uploads/photos
  chown 10001:10001 /data /uploads
  sqlite3 /data/arc.db "PRAGMA journal_mode=WAL; CREATE TABLE sample (id INTEGER PRIMARY KEY, body TEXT); INSERT INTO sample VALUES (1, '\''coffee'\'');"
  printf first > /uploads/photos/one
  printf second > /uploads/.two
  cp -a /uploads/. /expected/uploads/
  sqlite3 /data/arc.db "VACUUM INTO '\''/expected/arc.db'\'';"
'
compose up -d api writer
# Keep a real WAL connection open as the API does, with WAL/SHM on the RO mount.
for attempt in 1 2 3 4 5; do
  if compose run --rm -T backup test -f /data/arc.db-shm; then break; fi
  [ "$attempt" -lt 5 ] || exit 1
  sleep 1
done
# Exercise repository initialization, subsequent snapshots, and retention.
compose run --rm -T backup backup.sh
compose run --rm -T backup backup.sh
# Capture optional success/failure pings without contacting a monitoring service.
compose run --rm -T -e BACKUP_PING_URL=https://monitor.invalid/ping backup sh -es <<'SH'
mkdir /tmp/bin
cat > /tmp/bin/curl <<'MOCK'
#!/bin/sh
for arg do :; done
printf '%s\n' "$arg" > /expected/ping
MOCK
chmod +x /tmp/bin/curl
PATH=/tmp/bin:$PATH backup.sh
test "$(cat /expected/ping)" = https://monitor.invalid/ping
if RESTIC_PASSWORD=wrong PATH=/tmp/bin:$PATH backup.sh >/dev/null 2>&1; then exit 1; fi
test "$(cat /expected/ping)" = https://monitor.invalid/ping/fail
SH
# A failing backup must exit nonzero (and must not replace a usable snapshot).
if compose run --rm -T -e RESTIC_PASSWORD=wrong backup backup.sh >/dev/null 2>&1; then
  echo 'Backup unexpectedly accepted an incorrect password' >&2
  exit 1
fi
compose stop writer
compose -f "$work/seed.yml" run --rm -T backup sh -ec '
  rm -f /data/arc.db*
  find /uploads -mindepth 1 -maxdepth 1 -exec rm -rf {} \;
  printf stale > /uploads/stale
  touch /data/arc.db-wal /data/arc.db-shm
'
COMPOSE_PROJECT_NAME="$project" "$work/restore.sh" latest
compose run --rm -T backup sh -ec '
  cmp /expected/arc.db /data/arc.db
  diff -r /expected/uploads /uploads
  test ! -e /data/arc.db-wal
  test ! -e /data/arc.db-shm
'
# Failed snapshot lookup must leave the API running.
if COMPOSE_PROJECT_NAME="$project" "$work/restore.sh" does-not-exist >/dev/null 2>&1; then
  echo 'Restore unexpectedly accepted a missing snapshot' >&2
  exit 1
fi
[ -n "$(compose ps --status running -q api)" ]
# The real API uses uid 10001: it must be able to mutate the restored database.
compose -f "$work/seed.yml" run --rm -T --user 10001:10001 backup sh -ec '
  sqlite3 /data/arc.db "INSERT INTO sample VALUES (2, '\''restored'\'');"
  test -r /uploads/photos/one
  test -r /uploads/.two
  touch /uploads/new-file
'
echo 'Backup/restore round trip passed (database bytes and both upload files identical).'
