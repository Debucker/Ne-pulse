#!/usr/bin/env bash
set -euo pipefail

# NE-PULSE production deployment script.
#
# Run this on the target Ubuntu VPS (root/sudo) after cloning
# https://github.com/Debucker/Ne-pulse.git. Re-run any time to pull a fresh
# build and restart both services — it's idempotent.
#
# What it does, in order:
#   1. Installs Go, Node.js, Redis, and Caddy if not already present, and
#      creates an unprivileged service user.
#   2. Builds the Go binary (cmd/server -> ./bin/ne-pulse-server).
#   3. Installs frontend deps and builds the Next.js standalone production
#      output.
#   4. Installs systemd unit files for both processes and enables them so
#      they restart automatically on crash or server reboot.
#   5. Installs/reloads the Caddy reverse-proxy config (automatic HTTPS +
#      wss:// upgrade + ne-pulse.com -> 127.0.0.1:8080 / 127.0.0.1:3000).
#
# Requires /etc/ne-pulse/ne-pulse.env to already exist (copy it from
# .env.production.example and fill in real values first) — deploy.sh
# refuses to run without it rather than silently deploying with placeholder
# config.
#
# Usage: sudo ./deploy.sh

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
WEB_DIR="$REPO_ROOT/web"
BIN_DIR="$REPO_ROOT/bin"
DEPLOY_DIR="$REPO_ROOT/deploy"
ENV_FILE="/etc/ne-pulse/ne-pulse.env"
SERVICE_USER="${NE_PULSE_USER:-nepulse}"

log() { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }

require_root() {
  if [ "$(id -u)" -ne 0 ]; then
    echo "This script installs system packages and systemd units — run it with sudo." >&2
    exit 1
  fi
}

require_env_file() {
  if [ ! -f "$ENV_FILE" ]; then
    echo "Missing $ENV_FILE." >&2
    echo "Copy .env.production.example there and fill in real values first:" >&2
    echo "  mkdir -p /etc/ne-pulse && cp $REPO_ROOT/.env.production.example $ENV_FILE" >&2
    exit 1
  fi
}

install_runtime() {
  log "Installing/verifying runtime dependencies (Go, Node.js, Redis, Caddy)..."
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -y

  if ! command -v go >/dev/null 2>&1; then
    log "Installing Go 1.26..."
    curl -fsSL https://go.dev/dl/go1.26.5.linux-amd64.tar.gz -o /tmp/go.tar.gz
    rm -rf /usr/local/go
    tar -C /usr/local -xzf /tmp/go.tar.gz
    ln -sf /usr/local/go/bin/go /usr/local/bin/go
    ln -sf /usr/local/go/bin/gofmt /usr/local/bin/gofmt
    rm -f /tmp/go.tar.gz
  fi

  if ! command -v node >/dev/null 2>&1; then
    log "Installing Node.js 20 LTS..."
    curl -fsSL https://deb.nodesource.com/setup_20.x | bash -
    apt-get install -y nodejs
  fi

  if ! command -v redis-server >/dev/null 2>&1; then
    log "Installing Redis..."
    apt-get install -y redis-server
    systemctl enable --now redis-server
  fi

  if ! command -v caddy >/dev/null 2>&1; then
    log "Installing Caddy..."
    apt-get install -y debian-keyring debian-archive-keyring apt-transport-https curl gnupg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' \
      | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
    curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' \
      | tee /etc/apt/sources.list.d/caddy-stable.list
    apt-get update -y
    apt-get install -y caddy
  fi

  if ! id "$SERVICE_USER" >/dev/null 2>&1; then
    log "Creating unprivileged service user '$SERVICE_USER'..."
    useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
  fi
}

build_backend() {
  log "Building the Go backend (cmd/server)..."
  mkdir -p "$BIN_DIR"
  (cd "$REPO_ROOT" && go build -o "$BIN_DIR/ne-pulse-server" ./cmd/server)
  echo "  -> $BIN_DIR/ne-pulse-server"
}

build_frontend() {
  log "Building the Next.js frontend (standalone production output)..."
  (cd "$WEB_DIR" && npm ci && npm run build)
  # Next.js's standalone output intentionally omits static assets and
  # public/ (documented behavior) — copy them alongside server.js so
  # `node server.js` can actually serve the site.
  rm -rf "$WEB_DIR/.next/standalone/.next/static" "$WEB_DIR/.next/standalone/public"
  cp -r "$WEB_DIR/.next/static" "$WEB_DIR/.next/standalone/.next/static"
  cp -r "$WEB_DIR/public" "$WEB_DIR/.next/standalone/public"
  echo "  -> $WEB_DIR/.next/standalone/server.js"
}

install_systemd_units() {
  log "Installing systemd unit files..."
  sed \
    -e "s#{{REPO_ROOT}}#$REPO_ROOT#g" \
    -e "s#{{SERVICE_USER}}#$SERVICE_USER#g" \
    "$DEPLOY_DIR/ne-pulse-backend.service" > /etc/systemd/system/ne-pulse-backend.service

  sed \
    -e "s#{{WEB_DIR}}#$WEB_DIR#g" \
    -e "s#{{SERVICE_USER}}#$SERVICE_USER#g" \
    "$DEPLOY_DIR/ne-pulse-frontend.service" > /etc/systemd/system/ne-pulse-frontend.service

  chown -R "$SERVICE_USER":"$SERVICE_USER" "$REPO_ROOT"

  systemctl daemon-reload
  systemctl enable ne-pulse-backend.service ne-pulse-frontend.service
  systemctl restart ne-pulse-backend.service
  systemctl restart ne-pulse-frontend.service
}

install_caddy_config() {
  log "Installing Caddy reverse-proxy config..."
  # shellcheck disable=SC1090
  DOMAIN="$(grep -E '^DOMAIN=' "$ENV_FILE" | cut -d= -f2- || true)"
  DOMAIN="${DOMAIN:-ne-pulse.com}"
  mkdir -p /var/log/caddy
  sed "s#{{DOMAIN}}#$DOMAIN#g" "$DEPLOY_DIR/Caddyfile" > /etc/caddy/Caddyfile
  systemctl enable caddy
  systemctl reload caddy 2>/dev/null || systemctl restart caddy
}

require_root
require_env_file
install_runtime
build_backend
build_frontend
install_systemd_units
install_caddy_config

log "Deployment complete."
cat <<EOF
  Backend:  systemctl status ne-pulse-backend
  Frontend: systemctl status ne-pulse-frontend
  Caddy:    systemctl status caddy
  Logs:     journalctl -u ne-pulse-backend -f
  Verify:   ./deploy/verify-hardware.sh https://<your-domain>
EOF
