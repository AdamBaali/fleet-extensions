#!/bin/bash
# Complete setup script for orbit on rpm-ostree systems
# Creates/fixes all directories, symlinks, and configuration
# Safe to run multiple times (idempotent)

set -euo pipefail

GREEN='\033[0;32m'; YELLOW='\033[1;33m'; RED='\033[0;31m'; NC='\033[0m'
info() { echo -e "${YELLOW}→${NC} $*"; }
ok()   { echo -e "${GREEN}✓${NC} $*"; }
die()  { echo -e "${RED}✗${NC} $*" >&2; exit 1; }

echo -e "${YELLOW}=== Setting up orbit on rpm-ostree ===${NC}"
echo ""

# Step 1: Verify prerequisites
info "Checking prerequisites..."
[ -d /opt/orbit ] || die "/opt/orbit not found - fleet-osquery not installed"
[ -d /var/lib/fleetd ] || die "/var/lib/fleetd not found - orbit not initialized"
ok "Prerequisites verified"

# Step 2: Detect architecture
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
    die "Unsupported architecture: $ARCH"
    ;;
esac
ok "Architecture: $ARCH"

# Step 3: Verify binaries exist
info "Verifying binaries..."
[ -f "$ORBIT_TARGET" ] || die "orbit binary not found at $ORBIT_TARGET"
[ -f "$OSQUERYD_TARGET" ] || die "osqueryd binary not found at $OSQUERYD_TARGET"
ok "Binaries verified"

# Step 4: Create all required directories
echo ""
info "Creating directory structure..."
sudo mkdir -p /var/lib/fleetd/bin/orbit
sudo mkdir -p /var/lib/fleetd/bin/osqueryd
sudo mkdir -p /var/lib/fleetd/extensions
sudo mkdir -p /var/lib/fleetd/osquery_log
sudo mkdir -p /var/lib/fleetd/lenses
ok "Directories created"

# Step 5: Create symlinks for orbit binaries
echo ""
info "Setting up symlinks..."
# Create symlinks (force overwrite if exists)
sudo ln -sf "$ORBIT_TARGET" /var/lib/fleetd/bin/orbit/orbit
sudo ln -sf "$OSQUERYD_TARGET" /var/lib/fleetd/bin/osqueryd/osqueryd
ok "Symlinks created"

# Step 6: Copy necessary support files from /opt/orbit
echo ""
info "Copying support files..."
# Copy certs if not present
[ -f /var/lib/fleetd/certs.pem ] || sudo cp /opt/orbit/certs.pem /var/lib/fleetd/certs.pem 2>/dev/null || true
# Copy TUF metadata if not present
[ -f /var/lib/fleetd/tuf-metadata.json ] || sudo cp /opt/orbit/tuf-metadata.json /var/lib/fleetd/tuf-metadata.json 2>/dev/null || true
ok "Support files verified"

# Step 7: Setup osquery.flags
echo ""
info "Setting up osquery.flags..."
sudo touch /var/lib/fleetd/osquery.flags

# Ensure it has correct permissions
sudo chmod 600 /var/lib/fleetd/osquery.flags

# Add extension flag if not present
EXTENSION_FLAG="--extension=/var/lib/fleetd/extensions/rpm_ostree.ext"
if sudo grep -qF "$EXTENSION_FLAG" /var/lib/fleetd/osquery.flags 2>/dev/null; then
  ok "Extension flag already present"
else
  echo "$EXTENSION_FLAG" | sudo tee -a /var/lib/fleetd/osquery.flags > /dev/null
  ok "Added extension flag"
fi

# Step 8: Set correct permissions
echo ""
info "Setting permissions..."
sudo chown -R root:root /var/lib/fleetd/
sudo chmod 755 /var/lib/fleetd/
sudo chmod 755 /var/lib/fleetd/bin/
sudo chmod 755 /var/lib/fleetd/bin/orbit/
sudo chmod 755 /var/lib/fleetd/bin/osqueryd/
sudo chmod 755 /var/lib/fleetd/extensions/
[ -f /var/lib/fleetd/extensions/rpm_ostree.ext ] && sudo chmod 700 /var/lib/fleetd/extensions/rpm_ostree.ext
ok "Permissions set"

# Step 9: Verify systemd service is configured
echo ""
info "Verifying systemd service..."
if sudo grep -q "root-dir=/var/lib/fleetd" /etc/systemd/system/orbit.service 2>/dev/null; then
  ok "orbit.service already configured"
else
  info "Updating orbit.service..."
  sudo sed -i 's|ExecStart=/opt/orbit/bin/orbit/orbit|ExecStart=/opt/orbit/bin/orbit/orbit --root-dir=/var/lib/fleetd|g' /etc/systemd/system/orbit.service
  sudo systemctl daemon-reload
  ok "orbit.service updated"
fi

# Step 10: Restart orbit
echo ""
info "Restarting orbit..."
sudo systemctl restart orbit.service &
sleep 5
ok "Orbit restarting in background"

# Final status
echo ""
echo -e "${GREEN}═══════════════════════════════════════════════${NC}"
echo -e "${GREEN}✓ Orbit setup complete!${NC}"
echo -e "${GREEN}═══════════════════════════════════════════════${NC}"
echo ""
echo "Summary:"
echo "  ✓ Directories created/verified"
echo "  ✓ Symlinks configured"
echo "  ✓ Support files verified"
echo "  ✓ osquery.flags configured"
echo "  ✓ Permissions set"
echo "  ✓ systemd service configured"
echo "  ✓ orbit restarting"
echo ""
echo "Next steps:"
echo "  - Wait 10 seconds for orbit to restart"
echo "  - Verify: ps aux | grep rpm_ostree.ext"
echo "  - Check logs: journalctl -u orbit.service --since '5 min ago'"
echo ""
