#!/bin/sh
# Verify the pre-deploy gate without a VPS, network, images, or real credentials.
set -eu
cd "$(dirname "$0")/../.."
script=$(pwd)/deploy/env/deploy.sh
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
trap 'exit 1' HUP INT TERM
mkdir "$work/bin"
cp "$script" "$work/deploy.sh"
cat > "$work/bin/docker" <<'MOCK'
#!/bin/sh
printf '%s\n' "$*" >> "$TEST_CALLS"
case " $* " in
  # The probe "is there no database yet?": 0 means yes, a first release.
  *' run '*' sh -c '*) exit "${PROBE_RESULT:-1}" ;;
  *' run '*) exit "${BACKUP_RESULT:-0}" ;;
esac
MOCK
cat > "$work/bin/curl" <<'MOCK'
#!/bin/sh
printf '{"version":"new"}\n'
MOCK
chmod +x "$work/bin/"*
export PATH="$work/bin:$PATH"
export TEST_CALLS="$work/calls"
for scenario in prod-fail prod-ok prod-first staging disabled; do
  printf "TAG='old'\n" > "$work/.env"
  cp "$work/.env" "$work/expected.env"
  echo old > "$work/.current_tag"
  : > "$TEST_CALLS"
  env_name=prod
  profiles=backup
  BACKUP_RESULT=0
  PROBE_RESULT=1
  case "$scenario" in
    prod-fail) BACKUP_RESULT=1 ;;
    # No arc.db yet. A backup would fail, so reaching it would stop this deploy.
    prod-first) PROBE_RESULT=0; BACKUP_RESULT=1 ;;
    staging) env_name=staging ;;
    disabled) profiles= ;;
  esac
  export BACKUP_RESULT PROBE_RESULT
  printf "APP_ENV='%s'\nCOMPOSE_PROFILES='%s'\nWEB_PORT='8081'\n" "$env_name" "$profiles" > "$work/.env.new"
  if [ "$scenario" = prod-fail ]; then
    if bash "$work/deploy.sh" new; then
      echo 'Failed backup unexpectedly allowed deployment' >&2
      exit 1
    fi
    cmp "$work/.env" "$work/expected.env"
    [ "$(cat "$work/.current_tag")" = old ]
    test ! -e "$work/.env.new"
    if grep -q ' up ' "$TEST_CALLS"; then exit 1; fi
  else
    bash "$work/deploy.sh" new
    [ "$(cat "$work/.current_tag")" = new ]
  fi
  if [ "$profiles" = backup ] && [ "$env_name" = prod ]; then
    grep -q 'pull --quiet' "$TEST_CALLS"
    # The probe is the second Docker operation, immediately after pull.
    sed -n '2p' "$TEST_CALLS" | grep -q 'run --rm -T backup sh -c'
    if [ "$scenario" = prod-first ]; then
      # First release: no snapshot, and the switch still happens.
      if grep -q 'backup backup.sh' "$TEST_CALLS"; then exit 1; fi
      grep -q ' up ' "$TEST_CALLS"
    else
      # Otherwise the backup is next, before anything running is touched.
      sed -n '3p' "$TEST_CALLS" | grep -q 'run --rm -T backup backup.sh'
    fi
  else
    if grep -q ' run ' "$TEST_CALLS"; then exit 1; fi
  fi
done
echo 'Deployment gate tests passed.'
