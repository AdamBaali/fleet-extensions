#!/bin/bash
# Deploy the rpm_ostree_deployments osquery extension to an rpm-ostree host
# (Fedora Silverblue / CoreOS / Universal Blue, etc.) running Fleet's orbit.
#
# Usage:   ./deploy.sh <host> [user] [sudo-password]
# Example: ./deploy.sh my-host myuser
#          ./deploy.sh my-host myuser 'sudo-pass'   # non-interactive sudo
#
# - The matching binary (amd64/arm64) is chosen from the host's architecture.
# - osquery.flags is updated idempotently, so it is safe to re-run.
# - If no sudo password is given, sudo prompts interactively over SSH.

set -euo pipefail

HOST="${1:?Usage: $0 <host> [user] [sudo-password]}"
USER="${2:-$(whoami)}"
PASSWORD="${3:-}"

REMOTE_PATH="/var/lib/fleetd/extensions/rpm_ostree.ext"
OSQUERY_FLAGS="/var/lib/fleetd/osquery.flags"
TARGET="$USER@$HOST"
SSH=(ssh -o StrictHostKeyChecking=accept-new -o ConnectTimeout=15)

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info() { echo -e "${YELLOW}→${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }
die()  { echo -e "${RED}✗${NC} $*" >&2; exit 1; }

echo -e "${YELLOW}=== rpm_ostree extension deployment → ${TARGET} ===${NC}"

# 1. Pick the binary that matches the remote architecture.
ARCH=$("${SSH[@]}" "$TARGET" 'uname -m' | tr -d '[:space:]')
case "$ARCH" in
  aarch64|arm64) BINARY="./rpm_ostree-arm64.ext" ;;
  x86_64|amd64)  BINARY="./rpm_ostree-amd64.ext" ;;
  *) die "unsupported architecture: $ARCH" ;;
esac
[ -f "$BINARY" ] || die "$BINARY not found — run 'make all' first"
ok "host arch ${ARCH} → ${BINARY}"

# 2. Copy the binary and a one-shot install script to the host.
info "copying binary to ${HOST}:/tmp"
scp -o StrictHostKeyChecking=accept-new -q "$BINARY" "$TARGET:/tmp/rpm_ostree.ext"

INSTALL_SCRIPT=$(mktemp)
trap 'rm -f "$INSTALL_SCRIPT"' EXIT
cat > "$INSTALL_SCRIPT" <<EOF
set -e
install -o root -g root -m 0700 /tmp/rpm_ostree.ext "$REMOTE_PATH"
rm -f /tmp/rpm_ostree.ext
touch "$OSQUERY_FLAGS"
grep -qF -- "--extension=$REMOTE_PATH" "$OSQUERY_FLAGS" || echo "--extension=$REMOTE_PATH" >> "$OSQUERY_FLAGS"
systemctl restart orbit.service
EOF
scp -o StrictHostKeyChecking=accept-new -q "$INSTALL_SCRIPT" "$TARGET:/tmp/rpm_ostree_install.sh"
ok "binary and installer copied"

# 3. Run the install script as root (install + idempotent flag + orbit restart).
info "installing extension and restarting orbit (sudo)"
if [ -n "$PASSWORD" ]; then
  "${SSH[@]}" "$TARGET" "echo '$PASSWORD' | sudo -S -p '' bash /tmp/rpm_ostree_install.sh; rm -f /tmp/rpm_ostree_install.sh"
else
  "${SSH[@]}" -t "$TARGET" "sudo bash /tmp/rpm_ostree_install.sh; rm -f /tmp/rpm_ostree_install.sh"
fi
ok "installed and orbit restarted"

# 4. Wait for the extension to register with osquery.
info "waiting for the extension to register (up to 60s)…"
for _ in $(seq 1 12); do
  sleep 5
  if "${SSH[@]}" "$TARGET" "ps aux | grep -F '$REMOTE_PATH' | grep -vq grep"; then
    ok "extension process running"
    echo ""
    ok "Done. Query it from Fleet:  SELECT * FROM rpm_ostree_deployments;"
    exit 0
  fi
done
die "extension not running after 60s — check: journalctl -u orbit.service | grep rpm_ostree"
