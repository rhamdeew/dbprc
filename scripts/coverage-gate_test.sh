#!/usr/bin/env bash
# Tests for scripts/coverage-gate.sh. Runs from `make cover-backend`.
set -euo pipefail
export LC_ALL=C
export PATH="${HOME}/.local/go/bin:${HOME}/.local/bin:${PATH}"

root=$(cd "$(dirname "$0")/.." && pwd)
gate="$root/scripts/coverage-gate.sh"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

assert_fail() {
  local err="$tmp/assert-err"
  if "$@" >"$tmp/assert-out" 2>"$err"; then
    fail "expected failure: $*"
  fi
}

# --- numeric boundary (check) ---
"$gate" check 80 80 "boundary" >/dev/null
"$gate" check 80.0 80 "boundary" >/dev/null
"$gate" check 90 70 "above" >/dev/null
assert_fail "$gate" check 79.9 80 "below"
grep -q "coverage gate failed: below 79.9% < 80%" "$tmp/assert-err" || fail "missing boundary error"

# The headline is recorded before the non-zero exit, including in CI summaries.
export GITHUB_STEP_SUMMARY="$tmp/summary.md"
assert_fail "$gate" check 10 70 "Frontend lines"
grep -q "Frontend lines: 10% (minimum 70%)" "$tmp/summary.md" || fail "summary not written on failure"
unset GITHUB_STEP_SUMMARY

# --- Vitest v8 line percentage ---
cat >"$tmp/ok.json" <<'EOF'
{"total":{"lines":{"total":10,"covered":7,"skipped":0,"pct":70},"statements":{"pct":1},"functions":{"pct":1},"branches":{"pct":1}}}
EOF
"$gate" vitest "$tmp/ok.json" 70 >/dev/null
assert_fail "$gate" vitest "$tmp/ok.json" 70.1

cat >"$tmp/high.json" <<'EOF'
{"total":{"lines":{"total":8,"covered":8,"skipped":0,"pct":100}}}
EOF
out=$("$gate" vitest "$tmp/high.json" 70)
grep -q "Vitest v8 line coverage: \*\*100.0%\*\* (minimum 70%)" <<<"$out" || fail "vitest summary line: $out"

echo '{"total":{"lines":{"pct":"Unknown"}}}' >"$tmp/unknown.json"
assert_fail "$gate" vitest "$tmp/unknown.json" 70
echo '{"total":{}}' >"$tmp/bad.json"
assert_fail "$gate" vitest "$tmp/bad.json" 70

# --- Go coverprofile: filter, then the same total as `go tool cover` ---
mod="$tmp/mod"
mkdir -p "$mod/internal/keep" "$mod/internal/db/sqlc" "$mod/cmd/tool"
(
  cd "$mod"
  go mod init example.com/covergate >"$tmp/gomod.out" 2>&1
)

cat >"$mod/internal/keep/keep.go" <<'EOF'
package keep

func Covered() int { return 1 }

func Uncovered() int {
	n := 0
	n++
	n++
	n++
	n++
	return n
}
EOF
cat >"$mod/internal/keep/keep_test.go" <<'EOF'
package keep

import "testing"

func TestCovered(t *testing.T) {
	if Covered() != 1 {
		t.Fatal(Covered())
	}
}
EOF

# Large uncovered functions. They must not affect the total once excluded.
cat >"$mod/internal/db/sqlc/gen.go" <<'EOF'
package sqlc

func Generated() int {
	n := 0
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	return n
}
EOF
cat >"$mod/internal/db/sqlc/gen_test.go" <<'EOF'
package sqlc

import "testing"

func TestLinked(t *testing.T) {}
EOF
cat >"$mod/cmd/tool/main.go" <<'EOF'
package main

func wire() int {
	n := 0
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	n++
	return n
}

func main() { println(wire()) }
EOF
cat >"$mod/cmd/tool/main_test.go" <<'EOF'
package main

import "testing"

func TestLinked(t *testing.T) {}
EOF

(
  cd "$mod"
  go test -count=1 -coverprofile=coverage.out ./... >"$tmp/gotest.out"
)

exclude='/(internal/db/sqlc|cmd)/'
awk -v re="$exclude" 'NR == 1 { next } re != "" && $0 ~ re { next } { print }' "$mod/coverage.out" >"$tmp/filtered-body"
oracle=$(awk '
  {
    n = split($0, f, " ")
    stmts = f[n - 1] + 0
    count = f[n] + 0
    total += stmts
    if (count > 0) covered += stmts
  }
  END {
    if (total == 0) exit 1
    printf "%.1f\n", (100 * covered) / total
  }
' "$tmp/filtered-body")

out=$("$gate" go "$mod/coverage.out" 0 "$exclude")
grep -q "Go statement coverage: \*\*${oracle}%\*\*" <<<"$out" || fail "got $(grep 'Go statement coverage' <<<"$out") want ${oracle}%"

# Excluding the uncovered packages must raise the total.
unfiltered=$(cd "$mod" && go tool cover -func=coverage.out | awk '/^total:/ { gsub("%", "", $NF); print $NF }')
awk -v a="$oracle" -v b="$unfiltered" 'BEGIN { exit !((a + 0) > (b + 0)) }' || fail "exclude did not raise coverage (${oracle} vs ${unfiltered})"

"$gate" go "$mod/coverage.out" "$oracle" "$exclude" >/dev/null
next=$(awk -v p="$oracle" 'BEGIN { printf "%.1f\n", p + 0.1 }')
assert_fail "$gate" go "$mod/coverage.out" "$next" "$exclude"

# A profile whose every block is excluded is a hard failure, not 0%.
awk -v re="$exclude" 'NR == 1 || $0 ~ re' "$mod/coverage.out" >"$mod/only-excluded.out"
assert_fail "$gate" go "$mod/only-excluded.out" 0 "$exclude"
grep -q "no statements after exclusions" "$tmp/assert-err" || fail "missing empty-profile error"

echo "coverage-gate tests ok"
