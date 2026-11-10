#!/usr/bin/env bash
# ARC VPS provisioning. Idempotent. Run as root:
#   ssh root@ddr1 'bash -s' < deploy/provision.sh
set -euo pipefail

PROD_USER=www
STAGING_USER=staging
STAGING_HOME=/srv/staging
EDGE_DIR=/opt/arc-edge
PROD_HOST=arc.dbdemo.live
STAGING_HOST=starc.dbdemo.live
SSH_PORT="$(sshd -T 2>/dev/null | awk '/^port /{print $2; exit}')"

log() { printf '\n==> %s\n' "$*"; }

[[ $EUID -eq 0 ]] || { echo "run as root"; exit 1; }
[[ -n "$SSH_PORT" ]] || { echo "cannot detect sshd port"; exit 1; }

log "Packages"
export DEBIAN_FRONTEND=noninteractive
apt-get update -qq
apt-get install -y -qq ufw unattended-upgrades curl ca-certificates >/dev/null

log "Unattended upgrades"
cat >/etc/apt/apt.conf.d/20auto-upgrades <<'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
EOF
systemctl enable --now unattended-upgrades >/dev/null

log "Users"
id "$PROD_USER" >/dev/null
usermod -aG docker "$PROD_USER"
if ! id "$STAGING_USER" >/dev/null 2>&1; then
  useradd --create-home --home-dir "$STAGING_HOME" --shell /bin/bash --groups docker "$STAGING_USER"
fi
usermod -aG docker "$STAGING_USER"

# staging gets the same personal SSH key(s) as www (deploy keys are added later, separately)
PROD_HOME="$(getent passwd "$PROD_USER" | cut -d: -f6)"
install -d -m 700 -o "$STAGING_USER" -g "$STAGING_USER" "$STAGING_HOME/.ssh"
touch "$STAGING_HOME/.ssh/authorized_keys"
while IFS= read -r key; do
  [[ -z "$key" ]] && continue
  grep -qxF "$key" "$STAGING_HOME/.ssh/authorized_keys" || echo "$key" >>"$STAGING_HOME/.ssh/authorized_keys"
done <"$PROD_HOME/.ssh/authorized_keys"
chown "$STAGING_USER:$STAGING_USER" "$STAGING_HOME/.ssh/authorized_keys"
chmod 600 "$STAGING_HOME/.ssh/authorized_keys"

install -d -m 750 -o "$PROD_USER" -g "$PROD_USER" "$PROD_HOME/arc"
install -d -m 750 -o "$STAGING_USER" -g "$STAGING_USER" "$STAGING_HOME/arc"

log "Docker log rotation"
if [[ ! -f /etc/docker/daemon.json ]]; then
  cat >/etc/docker/daemon.json <<'EOF'
{
  "log-driver": "json-file",
  "log-opts": { "max-size": "10m", "max-file": "3" }
}
EOF
  systemctl restart docker
else
  echo "daemon.json exists, left untouched:"; cat /etc/docker/daemon.json
fi

log "Edge Caddy ($EDGE_DIR)"
install -d -m 755 "$EDGE_DIR"
cat >"$EDGE_DIR/Caddyfile" <<EOF
{
	servers {
		protocols h1 h2 h3
	}
}

(upstream_down) {
	handle_errors 502 503 504 {
		respond "ARC is not deployed here yet" 503
	}
}

$PROD_HOST {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8081
	import upstream_down
}

$STAGING_HOST {
	encode zstd gzip
	reverse_proxy 127.0.0.1:8082
	import upstream_down
}
EOF
cat >"$EDGE_DIR/compose.yml" <<'EOF'
name: arc-edge
services:
  caddy:
    image: caddy:2
    restart: unless-stopped
    network_mode: host
    volumes:
      - ./Caddyfile:/etc/caddy/Caddyfile:ro
      - caddy_data:/data
      - caddy_config:/config
volumes:
  caddy_data:
  caddy_config:
EOF
docker compose -f "$EDGE_DIR/compose.yml" up -d --pull always --quiet-pull </dev/null
docker compose -f "$EDGE_DIR/compose.yml" exec -T caddy caddy reload --config /etc/caddy/Caddyfile </dev/null >/dev/null 2>&1 || true

log "Firewall (ssh port $SSH_PORT)"
ufw --force default deny incoming >/dev/null
ufw --force default allow outgoing >/dev/null
ufw allow "$SSH_PORT/tcp" >/dev/null
ufw allow 80/tcp >/dev/null
ufw allow 443/tcp >/dev/null
ufw allow 443/udp >/dev/null
ufw --force enable >/dev/null
ufw status verbose

log "SSH hardening"
cat >/etc/ssh/sshd_config.d/10-arc-hardening.conf <<'EOF'
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin prohibit-password
EOF
sshd -t
systemctl reload ssh
sshd -T | grep -E '^(passwordauthentication|kbdinteractiveauthentication|permitrootlogin) '

log "Done"
