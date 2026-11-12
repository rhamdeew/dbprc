#!/bin/sh
set -eu
umask 077

finish() {
  result=$?
  trap - EXIT
  rm -f /tmp/arc.db
  if [ -n "${BACKUP_PING_URL:-}" ]; then
    suffix=
    [ "$result" -eq 0 ] || suffix=/fail
    # A monitoring outage must not hide the backup result or expose the ping URL.
    curl -fsS --max-time 20 --output /dev/null "${BACKUP_PING_URL}${suffix}" 2>/dev/null || true
  fi
  exit "$result"
}
trap finish EXIT
trap 'exit 1' HUP INT TERM

# Backend errors can include repository URLs; keep configured values out of logs.
run_restic() {
  if ! restic "$@" >/dev/null 2>&1; then
    echo "restic $1 failed; check repository access, password and storage" >&2
    return 1
  fi
}
restic snapshots >/dev/null 2>&1 || run_restic init
# Never reuse an incomplete snapshot from a previous invocation.
rm -f /tmp/arc.db
sqlite3 -readonly /data/arc.db ".timeout 30000" "VACUUM INTO '/tmp/arc.db';"
run_restic backup /tmp/arc.db /uploads --tag nightly --host arc
run_restic forget --keep-daily 7 --keep-weekly 4 --keep-monthly 6 --prune
echo 'Backup and retention completed.'
