#!/bin/sh
# Run against the actual Caddyfile in the web image, also during its CI build.
set -eu
caddy=${CADDY:-caddy}
config=${1:-deploy/env/Caddyfile}
export BASIC_AUTH_USER=validation
BASIC_AUTH_HASH=$("$caddy" hash-password --plaintext validation-only)
export BASIC_AUTH_HASH
for mode in on off '' unexpected; do
    export BASIC_AUTH_MODE="$mode"
    "$caddy" validate --config "$config" --adapter caddyfile
done
unset BASIC_AUTH_MODE
"$caddy" validate --config "$config" --adapter caddyfile
