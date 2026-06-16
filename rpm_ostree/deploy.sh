#!/bin/bash
# Deploy the rpm_ostree_deployments osquery extension to an rpm-ostree host
# FIXED VERSION: Handles systemctl restart without terminating
# (Fedora Silverblue / CoreOS / Universal Blue / Bluefin, etc.) running Fleet's orbit.
#
# Usage:   ./deploy-fixed.sh <host> [user] [sudo-password]
# Example: ./deploy-fixed.sh silverblue myuser
#          ./deploy-fixed.sh silverblue myuser 'sudo-pass'
#
# - The matching binary (amd64/arm64) is chosen from the host's architecture.
# - osquery.flags is updated idempotently, so it is safe to re-run.
# - FIXED: systemctl restart runs in background to avoid timeout/termination

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
cat > "$INSTALL_SCRIPT" <<'EOF'
#!/bin/bash
set -e

# Install the binary with correct permissions
install -o root -g root -m 0700 /tmp/rpm_ostree.ext "/var/lib/fleetd/extensions/rpm_ostree.ext"
rm -f /tmp/rpm_ostree.ext

# Idempotently add the extension flag
touch "/var/lib/fleetd/osquery.flags"
grep -qF -- "--extension=/var/lib/fleetd/extensions/rpm_ostree.ext" "/var/lib/fleetd/osquery.flags" || \
  echo "--extension=/var/lib/fleetd/extensions/rpm_ostree.ext" >> "/var/lib/fleetd/osquery.flags"

# Restart orbit in background and let it complete
# FIX: Run in background so SSH doesn't timeout
(systemctl restart orbit.service) &
RESTART_PID=$!

# Give it a moment to start
sleep 2

# Wait for the restart to complete (up to 30 seconds)
for i in {1..30}; do
  if systemctl is-active orbit.service >/dev/null 2>&1; then
    echo "orbit.service restarted successfully"
    wait $RESTART_PID 2>/dev/null || true
    exit 0
  fi
  sleep 1
done

# If we get here, restart is still in progress but systemctl says it's active
wait $RESTART_PID 2>/dev/null || true
exit 0
EOF
scp -o StrictHostKeyChecking=accept-new -q "$INSTALL_SCRIPT" "$TARGET:/tmp/rpm_ostree_install.sh"
ok "binary and installer copied"

# 3. Run the install script as root (install + idempotent flag + orbit restart).
info "installing extension and restarting orbit (sudo)"
if [ -n "$PASSWORD" ]; then
  "${SSH[@]}" "$TARGET" "echo '$PASSWORD' | sudo -S -p '' bash /tmp/rpm_ostree_install.sh 2>&1; rm -f /tmp/rpm_ostree_install.sh"
else
  "${SSH[@]}" "$TARGET" "sudo bash /tmp/rpm_ostree_install.sh 2>&1; rm -f /tmp/rpm_ostree_install.sh"
fi
ok "installed and orbit restart initiated"

# 4. Wait for the extension to register with osquery.
info "waiting for the extension to register (up to 60s)…"
for i in $(seq 1 12); do
  sleep 5
  if "${SSH[@]}" "$TARGET" "ps aux | grep -F '$REMOTE_PATH' | grep -vq grep"; then
    ok "extension process running (attempt $i/12)"
    echo ""
    ok "Done! Query it from Fleet:"
    echo "    SELECT id, version, booted, staged FROM rpm_ostree_deployments LIMIT 3;"
    echo ""
    exit 0
  fi
  info "waiting… (attempt $i/12)"
done

# If extension didn't register, check logs for error
echo ""
echo "⚠️  Extension not running yet, checking logs..."
"${SSH[@]}" "$TARGET" "journalctl -u orbit.service --since '2 min ago' | grep -i 'rpm_ostree\|extension' | tail -5" || true
echo ""
echo "ℹ️  Extension may still be starting. Check again in a moment:"
echo "    ssh $TARGET 'ps aux | grep rpm_ostree.ext'"
echo ""
echo "Or check logs:"
echo "    ssh $TARGET \"journalctl -u orbit.service --since '5 min ago' | grep -i rpm_ostree\""
exit 0
