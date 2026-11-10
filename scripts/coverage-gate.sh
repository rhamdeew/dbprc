#!/usr/bin/env bash
# Enforce coverage thresholds. The minimums live in the Makefile and are passed
# in; this script only measures and compares.
#
#   coverage-gate.sh check  <percent> <minimum> <label>
#   coverage-gate.sh go     <coverprofile> <minimum> <exclude-ere>
#   coverage-gate.sh vitest <coverage-summary.json> <minimum>
#
# `go` reads a `go test -coverprofile` file, drops paths matching <exclude-ere>,
# and uses `go tool cover -func` for the statement total. `vitest` reads the
# v8 json-summary and checks lines.pct.
#
# When GITHUB_STEP_SUMMARY is set, the same headline is appended there so the
# GitHub job summary shows the figure even if the gate then fails.
set -euo pipefail
export LC_ALL=C

filtered=""
cleanup() {
  if [[ -n "$filtered" ]]; then
    rm -f "$filtered"
  fi
}
trap cleanup EXIT

note() {
  printf '%s\n' "$1"
  if [[ -n "${GITHUB_STEP_SUMMARY:-}" ]]; then
    printf '%s\n' "$1" >> "${GITHUB_STEP_SUMMARY}"
  fi
}

# Fail when percent < minimum. Equal to the minimum passes.
fail_if_below() {
  local pct="$1" min="$2" label="$3"
  local below
  below=$(awk -v p="$pct" -v m="$min" 'BEGIN { print ((p + 0) < (m + 0)) ? "yes" : "no" }')
  if [[ "$below" == "yes" ]]; then
    echo "coverage gate failed: ${label} ${pct}% < ${min}%" >&2
    exit 1
  fi
}

usage() {
  echo "usage: coverage-gate.sh check <percent> <minimum> <label>" >&2
  echo "       coverage-gate.sh go <coverprofile> <minimum> <exclude-ere>" >&2
  echo "       coverage-gate.sh vitest <coverage-summary.json> <minimum>" >&2
  exit 2
}

cmd_check() {
  local pct="$1" min="$2" label="$3"
  note "${label}: ${pct}% (minimum ${min}%)"
  fail_if_below "$pct" "$min" "$label"
}

module_root() {
  local d
  d=$(cd "$(dirname "$1")" && pwd)
  while [[ "$d" != "/" ]]; do
    if [[ -f "$d/go.mod" ]]; then
      printf '%s\n' "$d"
      return 0
    fi
    d=$(dirname "$d")
  done
  echo "no go.mod above $(dirname "$1")" >&2
  return 1
}

cmd_go() {
  local profile="$1" min="$2" exclude_re="$3"
  local module lines func_out pct
  [[ -f "$profile" ]] || { echo "cover profile not found: $profile" >&2; exit 1; }
  profile=$(cd "$(dirname "$profile")" && pwd)/$(basename "$profile")
  module=$(module_root "$profile")
  filtered=$(mktemp)

  awk -v re="$exclude_re" '
    NR == 1 { print; next }
    re != "" && $0 ~ re { next }
    { print }
  ' "$profile" > "$filtered"

  lines=$(wc -l < "$filtered")
  if [[ "$lines" -lt 2 ]]; then
    echo "cover profile has no statements after exclusions (${exclude_re})" >&2
    exit 1
  fi

  if ! func_out=$(cd "$module" && go tool cover -func="$filtered"); then
    echo "go tool cover failed" >&2
    exit 1
  fi
  printf '%s\n' "$func_out"
  pct=$(printf '%s\n' "$func_out" | awk '/^total:/ { gsub("%", "", $NF); print $NF; found = 1 } END { if (!found) exit 1 }')
  note "### Backend coverage"
  note "Go statement coverage: **${pct}%** (minimum ${min}%). Excludes profile paths matching \`${exclude_re}\`."
  fail_if_below "$pct" "$min" "Go statements"
}

cmd_vitest() {
  local summary="$1" min="$2"
  local pct
  [[ -f "$summary" ]] || { echo "vitest coverage summary not found: $summary" >&2; exit 1; }
  if ! pct=$(python3 - "$summary" <<'PY'
import json, sys
path = sys.argv[1]
try:
    doc = json.load(open(path))
    pct = doc["total"]["lines"]["pct"]
except (OSError, KeyError, TypeError, json.JSONDecodeError) as exc:
    print(exc, file=sys.stderr)
    sys.exit(2)
if isinstance(pct, bool) or not isinstance(pct, (int, float)):
    print("line pct is %r" % (pct,), file=sys.stderr)
    sys.exit(2)
print(pct)
PY
  ); then
    echo "vitest line coverage is missing or not a number" >&2
    exit 1
  fi
  pct=$(awk -v p="$pct" 'BEGIN { printf "%.1f\n", p + 0 }')
  note "### Frontend coverage"
  note "Vitest v8 line coverage: **${pct}%** (minimum ${min}%)."
  fail_if_below "$pct" "$min" "Frontend lines"
}

[[ $# -ge 1 ]] || usage
mode=$1
shift
case "$mode" in
  check)  [[ $# -eq 3 ]] || usage; cmd_check "$@" ;;
  go)     [[ $# -eq 3 ]] || usage; cmd_go "$@" ;;
  vitest) [[ $# -eq 2 ]] || usage; cmd_vitest "$@" ;;
  *) usage ;;
esac
