#!/usr/bin/env bash
# One-time (re-runnable) GitHub Environment setup for ARC deploys.
# Usage: deploy/setup-github.sh staging|prod
# Needs: gh logged in as a repo admin, root SSH access to the VPS, htpasswd, openssl.
set -euo pipefail

REPO=rhamdeew/dbprc
VPS_IP=192.227.229.38
SSH_PORT=1898

ENV="${1:-}"
case "$ENV" in
  staging) SSH_USER=staging; APP_HOST=starc.dbdemo.live; BRANCH=staging ;;
  prod)    SSH_USER=www;     APP_HOST=arc.dbdemo.live;   BRANCH=prod ;;
  *) echo "usage: $0 staging|prod"; exit 1 ;;
esac

step() { printf '\n==> %s\n' "$*"; }
secret() { printf '%s' "$2" | gh secret set "$1" --env "$ENV" --repo "$REPO"; }
var() { gh variable set "$1" --env "$ENV" --repo "$REPO" --body "$2"; }

step "Checking GitHub permissions"
if [[ "$(gh api "repos/$REPO" --jq .permissions.admin)" != "true" ]]; then
  echo "gh account '$(gh api user --jq .login)' is not an admin of $REPO."
  echo "Fix: gh auth login (as the repo owner), or gh auth switch, then re-run."
  exit 1
fi

step "Environment '$ENV' (deploys only from branch '$BRANCH')"
gh api -X PUT "repos/$REPO/environments/$ENV" --input - >/dev/null <<EOF
{"deployment_branch_policy":{"protected_branches":false,"custom_branch_policies":true}}
EOF
gh api -X POST "repos/$REPO/environments/$ENV/deployment-branch-policies" \
  -f name="$BRANCH" -f type=branch >/dev/null 2>&1 || true   # already exists → fine

step "Deploy key for $SSH_USER@$VPS_IP (old ARC deploy key for this env is replaced)"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
comment="gha-deploy-$ENV-arc"
ssh-keygen -q -t ed25519 -N '' -C "$comment" -f "$tmp/key"
pub="restrict $(cat "$tmp/key.pub")"
ssh -p "$SSH_PORT" "root@$VPS_IP" "set -e
  f=\$(getent passwd $SSH_USER | cut -d: -f6)/.ssh/authorized_keys
  sed -i '/ $comment\$/d' \"\$f\"
  echo '$pub' >> \"\$f\""
ssh -i "$tmp/key" -o IdentitiesOnly=yes -o BatchMode=yes -p "$SSH_PORT" "$SSH_USER@$VPS_IP" 'docker ps >/dev/null && echo "deploy key works"'
secret SSH_PRIVATE_KEY "$(cat "$tmp/key")"
secret SSH_KNOWN_HOSTS "$(ssh-keyscan -p "$SSH_PORT" "$VPS_IP" 2>/dev/null)"
var SSH_HOST "$VPS_IP"
var SSH_PORT "$SSH_PORT"
var SSH_USER "$SSH_USER"
var APP_HOST "$APP_HOST"

step "Basic Auth"
read -r -p "Basic Auth username for $ENV [arc]: " ba_user; ba_user="${ba_user:-arc}"
ba_pass="$(openssl rand -base64 24 | tr -d '/+=' | cut -c1-20)"
ba_hash="$(htpasswd -nbBC 12 "" "$ba_pass" | cut -d: -f2 | tr -d '\n')"
secret BASIC_AUTH_USER "$ba_user"
secret BASIC_AUTH_HASH "$ba_hash"

step "App secrets"
secret SESSION_SECRET "$(openssl rand -hex 32)"
read -r -p "Company Google Workspace domain (ALLOWED_DOMAIN, e.g. company.com): " allowed_domain
secret ALLOWED_DOMAIN "$allowed_domain"
read -r -p "Admin email(s), comma-separated (ADMIN_EMAILS): " admin_emails
secret ADMIN_EMAILS "$admin_emails"
read -r -p "Google OAuth client ID for $ENV (Enter = skip for now): " oidc_id
if [[ -n "$oidc_id" ]]; then
  read -r -s -p "Google OAuth client secret (hidden): " oidc_secret; echo
  secret OIDC_CLIENT_ID "$oidc_id"
  secret OIDC_CLIENT_SECRET "$oidc_secret"
else
  echo "Skipped. Later: re-run this script, or: gh secret set OIDC_CLIENT_ID --env $ENV --repo $REPO (and OIDC_CLIENT_SECRET)"
fi

step "Result"
gh secret list --env "$ENV" --repo "$REPO"
gh variable list --env "$ENV" --repo "$REPO"
cat <<EOF

Save these in your password manager now (shown only once):
  URL:      https://$APP_HOST
  Username: $ba_user
  Password: $ba_pass
EOF
