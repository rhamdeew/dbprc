#!/usr/bin/env bash
# Installs the pinned dev toolchain into ~/.local (idempotent, ~1-2 min in a fresh container).
#   scripts/toolchain.sh            # everything: go, golangci-lint, sqlc, Go modules, npm deps
#   scripts/toolchain.sh sqlc       # only the named tools (go | golangci-lint | sqlc | deps)
# Versions: toolchain.env; Go from the `go` line in backend/go.mod.
# Make targets put ~/.local/go/bin and ~/.local/bin on PATH automatically.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../toolchain.env
source "$root/toolchain.env"
GO_VERSION="$(awk '/^go /{print $2; exit}' "$root/backend/go.mod")"

prefix="${TOOLCHAIN_PREFIX:-$HOME/.local}"
bin="$prefix/bin"
mkdir -p "$bin"
export PATH="$prefix/go/bin:$bin:$PATH"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$(uname -m)" in
  x86_64 | amd64) arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "unsupported arch $(uname -m)" >&2; exit 1 ;;
esac

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fetch() { curl -fsSL --retry 3 --retry-delay 2 -o "$2" "$1"; }

install_go() {
  if [[ "$(go env GOVERSION 2>/dev/null || true)" != "go$GO_VERSION" ]]; then
    fetch "https://go.dev/dl/go$GO_VERSION.$os-$arch.tar.gz" "$tmp/go.tgz"
    rm -rf "$prefix/go"
    tar -xzf "$tmp/go.tgz" -C "$prefix"
  fi
  printf '%-14s %s\n' go "$(go env GOVERSION)"
}

install_golangci_lint() {
  local v="$GOLANGCI_LINT_VERSION" name
  if ! golangci-lint version 2>/dev/null | grep -q "version $v "; then
    name="golangci-lint-$v-$os-$arch"
    fetch "https://github.com/golangci/golangci-lint/releases/download/v$v/$name.tar.gz" "$tmp/gl.tgz"
    tar -xzf "$tmp/gl.tgz" -C "$tmp"
    install -m 0755 "$tmp/$name/golangci-lint" "$bin/golangci-lint"
  fi
  printf '%-14s %s\n' golangci-lint "$(golangci-lint version 2>/dev/null | awk '{print $4}')"
}

install_sqlc() {
  local v="$SQLC_VERSION" base
  if [[ "$(sqlc version 2>/dev/null || true)" != "v$v" ]]; then
    base="https://github.com/sqlc-dev/sqlc/releases/download/v$v/sqlc_${v}_${os}_${arch}"
    if fetch "$base.tar.gz" "$tmp/sqlc.tgz" 2>/dev/null; then
      tar -xzf "$tmp/sqlc.tgz" -C "$tmp"
    else
      fetch "$base.zip" "$tmp/sqlc.zip"
      unzip -q -o "$tmp/sqlc.zip" -d "$tmp"
    fi
    install -m 0755 "$tmp/sqlc" "$bin/sqlc"
  fi
  printf '%-14s %s\n' sqlc "$(sqlc version)"
}

install_deps() {
  (cd "$root/backend" && go mod download)
  printf '%-14s %s\n' go-modules ok
  (cd "$root/frontend" && npm ci --no-audit --no-fund --loglevel=error --update-notifier=false >/dev/null)
  printf '%-14s %s\n' npm-deps "ok (node $(node --version))"
}

tools=("$@")
[[ ${#tools[@]} -eq 0 ]] && tools=(go golangci-lint sqlc deps)
for t in "${tools[@]}"; do
  case "$t" in
    go) install_go ;;
    golangci-lint) install_golangci_lint ;;
    sqlc) install_sqlc ;;
    deps) install_deps ;;
    *) echo "unknown tool: $t" >&2; exit 1 ;;
  esac
done
