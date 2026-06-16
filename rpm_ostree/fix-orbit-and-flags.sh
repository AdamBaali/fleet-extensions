#!/bin/bash
# Fix orbit symlinks and osquery flags on rpm-ostree systems
# Safe to run via Fleet - handles being executed within orbit itself

set -euo pipefail

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info() { echo -e "${YELLOW}→${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }

echo -e "${YELLOW}=== Fixing orbit symlinks and flags ===${NC}"
echo ""

# Step 1: Detect architecture
info "Detecting architecture..."
ARCH=$(uname -m | tr -d '[:space:]')
case "$ARCH" in
  aarch64|arm64)
    ORBIT_TARGET="/opt/orbit/bin/orbit/linux-arm64/stable/orbit"
    OSQUERYD_TARGET="/opt/orbit/bin/osqueryd/linux-arm64/stable/osqueryd"
    ;;
  x86_64|amd64)
    ORBIT_TARGET="/opt/orbit/bin/orbit/linux-amd64/stable/orbit"
    OSQUERYD_TARGET="/opt/orbit/bin/osqueryd/linux-amd64/stable/osqueryd"
    ;;
  *)
    echo -e "${RED}✗${NC} Unsupported architecture: $ARCH" >&2
    exit 1
    ;;
esac
ok "Architecture: $ARCH"

# Step 2: Check current state
echo ""
info "Checking current symlinks..."
ORBIT_OK=0
OSQUERYD_OK=0

if [ -L /var/lib/fleetd/bin/orbit/orbit ] 2>/dev/null; then
  ok "orbit/orbit is a symlink ✓"
  ORBIT_OK=1
else
  info "orbit/orbit needs fixing"
fi

if [ -L /var/lib/fleetd/bin/osqueryd/osqueryd ] 2>/dev/null; then
  ok "osqueryd/osqueryd is a symlink ✓"
  OSQUERYD_OK=1
else
  info "osqueryd/osqueryd needs fixing"
fi

# Step 3: Fix symlinks (can be done while orbit is running)
echo ""
info "Creating symlinks..."

# Remove old files if they exist
[ -e /var/lib/fleetd/bin/orbit/orbit ] && sudo rm -f /var/lib/fleetd/bin/orbit/orbit
[ -e /var/lib/fleetd/bin/osqueryd/osqueryd ] && sudo rm -f /var/lib/fleetd/bin/osqueryd/osqueryd

# Create symlinks
sudo ln -s "$ORBIT_TARGET" /var/lib/fleetd/bin/orbit/orbit
sudo ln -s "$OSQUERYD_TARGET" /var/lib/fleetd/bin/osqueryd/osqueryd
ok "Symlinks created"

# Step 4: Fix osquery.flags
echo ""
info "Checking osquery.flags..."
sudo touch /var/lib/fleetd/osquery.flags

EXTENSION_FLAG="--extension=/var/lib/fleetd/extensions/rpm_ostree.ext"
if grep -qF "$EXTENSION_FLAG" /var/lib/fleetd/osquery.flags 2>/dev/null; then
  ok "Extension flag already present"
else
  echo "$EXTENSION_FLAG" | sudo tee -a /var/lib/fleetd/osquery.flags > /dev/null
  ok "Added extension flag"
fi

# Step 5: Restart orbit in background
echo ""
info "Restarting orbit service (background)..."
sudo systemctl restart orbit.service &
RESTART_PID=$!
sleep 3
ok "Orbit restart initiated (PID: $RESTART_PID)"

# Step 6: Wait a bit for orbit to restart
echo ""
info "Waiting for orbit to restart..."
sleep 5

# Final status
echo ""
echo -e "${GREEN}═══════════════════════════════════════════════${NC}"
echo -e "${GREEN}✓ All fixes applied!${NC}"
echo -e "${GREEN}═══════════════════════════════════════════════${NC}"
echo ""
echo "Summary:"
echo "  ✓ Symlinks fixed"
echo "  ✓ Extension flag added"
echo "  ✓ orbit restarting in background"
echo ""
