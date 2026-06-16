# Complete Installation Guide - Fedora Silverblue 44

**Target:** Fresh Fedora Silverblue 44.0.0 system  
**Status:** Ready for installation  
**Time Required:** 20-25 minutes

---

## Prerequisites

Before starting, gather this information:

```bash
# 1. Test SSH connectivity
ssh <your-silverblue-hostname> "echo 'Connected!'"

# 2. Get the architecture (aarch64 or x86_64)
ssh <your-silverblue-hostname> "uname -m"

# 3. Confirm you're on Silverblue
ssh <your-silverblue-hostname> "cat /etc/os-release | grep PRETTY"
# Should show: Fedora Silverblue or similar

# 4. Have your Fleet enrollment secret ready (ask your Fleet admin if needed)
```

---

## Step 1: Download Fleet-OSQuery RPM

Replace `<ARCH>` with your architecture (`aarch64` or `x86_64`):

```bash
cd /tmp

# For ARM (aarch64 / arm64)
curl -L -o fleet-osquery-1.54.0.aarch64.rpm \
  "https://github.com/fleetdm/fleet/releases/download/fleet-osquery-1.54.0/fleet-osquery-1.54.0-1.aarch64.rpm"

# OR for x86_64 (amd64)
curl -L -o fleet-osquery-1.54.0.x86_64.rpm \
  "https://github.com/fleetdm/fleet/releases/download/fleet-osquery-1.54.0/fleet-osquery-1.54.0-1.x86_64.rpm"

# Verify download
ls -lh /tmp/fleet-osquery-1.54.0.*.rpm
```

---

## Step 2: Install as Layered Package

**Important:** Use `rpm-ostree install`, NOT regular `dnf install` (read-only filesystem)

```bash
# Copy RPM to the target host
scp /tmp/fleet-osquery-1.54.0.aarch64.rpm <hostname>:/tmp/

# Install via rpm-ostree (this REQUIRES a reboot)
ssh <hostname> << 'EOF'
set -e
echo "Installing fleet-osquery as layered package..."
sudo rpm-ostree install /tmp/fleet-osquery-1.54.0.aarch64.rpm

echo "Rebooting system..."
sudo systemctl reboot
EOF

# Wait for reboot (2-3 minutes)
echo "Waiting for system to reboot..."
sleep 120

# Verify it's back online
ssh <hostname> "echo 'System is back online'"
```

---

## Step 3: Prepare Writable State Directory

**CRITICAL:** Use `cp -aP` (preserve symlinks) NOT `cp -aL` (dereference)

```bash
ssh <hostname> << 'EOF'
set -e

echo "Creating writable state directory..."
sudo mkdir -p /var/lib/fleetd
sudo chown root:root /var/lib/fleetd
sudo chmod 755 /var/lib/fleetd

echo "Copying orbit files WITH SYMLINKS PRESERVED..."
sudo cp -aP /opt/orbit/. /var/lib/fleetd/
# ^ NOTE: -aP preserves symlinks (correct)
#        -aL dereferences symlinks (wrong - causes issues)

echo "Verifying symlinks were preserved..."
file /var/lib/fleetd/bin/orbit/orbit
file /var/lib/fleetd/bin/osqueryd/osqueryd
# Both should show: "symbolic link to ..."
# If they show "ELF 64-bit executable", you used -aL (WRONG)

EOF
```

**Expected output:**
```
/var/lib/fleetd/bin/orbit/orbit: symbolic link to /opt/orbit/bin/orbit/linux-arm64/stable/orbit
/var/lib/fleetd/bin/osqueryd/osqueryd: symbolic link to /opt/orbit/bin/osqueryd/linux-arm64/stable/osqueryd
```

**If you see "ELF 64-bit executable":** STOP and run:
```bash
bash /tmp/fix_orbit_symlinks.sh
```

---

## Step 4: Modify Orbit SystemD Service

Update the service to use `/var/lib/fleetd` for writable state:

```bash
ssh <hostname> << 'EOF'
set -e

echo "Modifying orbit.service..."

# Use sed to add the --root-dir flag
sudo sed -i 's|ExecStart=/opt/orbit/bin/orbit/orbit|ExecStart=/opt/orbit/bin/orbit/orbit --root-dir=/var/lib/fleetd|' \
  /etc/systemd/system/orbit.service

# Reload systemd configuration
sudo systemctl daemon-reload

echo "Verifying the change..."
grep "ExecStart=" /etc/systemd/system/orbit.service

EOF
```

**Should output:** `ExecStart=/opt/orbit/bin/orbit/orbit --root-dir=/var/lib/fleetd`

---

## Step 5: Start Orbit Service

```bash
ssh <hostname> << 'EOF'
set -e

echo "Enabling and starting orbit service..."
sudo systemctl enable orbit.service
sudo systemctl start orbit.service

echo "Waiting for startup..."
sleep 5

echo "Checking service status..."
systemctl status orbit.service --no-pager | head -15

echo "Checking for processes..."
ps aux | grep -E "orbit|osquery" | grep -v grep | wc -l

EOF
```

**Expected output:**
- Status should show: `Active: active (running)`
- Should show 3+ processes (orbit + osqueryd x2)

---

## Step 6: Verify Fleet Enrollment

```bash
ssh <hostname> << 'EOF'
set -e

echo "Checking enrollment..."
journalctl -u orbit.service --since "5 min ago" | grep -i "enroll\|registered" | head -5

echo ""
echo "Checking for errors..."
journalctl -u orbit.service --since "5 min ago" | grep -i "error\|fatal" | grep -v "authorized_keys\|drop privileges" || echo "No errors found ✅"

echo ""
echo "Checking for SYMLINK errors (should be empty)..."
journalctl -u orbit.service --since "5 min ago" | grep "symlink" || echo "No symlink errors ✅"

EOF
```

**Expected:** No symlink errors, enrollment messages visible.

---

## Step 7: Deploy Extension (Optional)

Deploy the `rpm_ostree_deployments` extension to see deployment status:

### Option A: Use the Fixed Deploy Script (Recommended)

```bash
cd ~/path/to/fleet-extensions/rpm_ostree

# Run the FIXED deploy script (handles restart properly)
./deploy-fixed.sh <hostname> <username>

# Example:
./deploy-fixed.sh silverblue.local adam
```

The script will:
1. ✅ Detect architecture
2. ✅ Download correct binary
3. ✅ Validate it
4. ✅ Install to `/var/lib/fleetd/extensions/`
5. ✅ Update osquery.flags
6. ✅ Restart orbit (properly, without terminating)
7. ✅ Wait for extension to register

### Option B: Manual Installation

```bash
# Copy the binary
scp ./rpm_ostree-aarch64.ext <hostname>:/tmp/

# Install manually
ssh <hostname> << 'EOF'
set -e

echo "Installing extension..."
sudo cp /tmp/rpm_ostree-aarch64.ext /var/lib/fleetd/extensions/rpm_ostree.ext
sudo chmod 700 /var/lib/fleetd/extensions/rpm_ostree.ext
sudo chown root:root /var/lib/fleetd/extensions/rpm_ostree.ext

echo "Adding flag to osquery.flags..."
echo "--extension=/var/lib/fleetd/extensions/rpm_ostree.ext" | sudo tee -a /var/lib/fleetd/osquery.flags

echo "Restarting orbit..."
sudo systemctl restart orbit.service
sleep 5

echo "Checking if extension is running..."
ps aux | grep "rpm_ostree.ext" | grep -v grep || echo "Extension starting..."

EOF
```

---

## Step 8: Verify Everything

```bash
ssh <hostname> << 'EOF'
set -e

echo "═══════════════════════════════════════════════════════════"
echo "VERIFICATION CHECKLIST"
echo "═══════════════════════════════════════════════════════════"

echo ""
echo "✓ Symlinks preserved (not dereferenced)"
file /var/lib/fleetd/bin/orbit/orbit
file /var/lib/fleetd/bin/osqueryd/osqueryd
echo "(both should show 'symbolic link to...')"

echo ""
echo "✓ orbit.service active"
systemctl is-active orbit.service

echo ""
echo "✓ Processes running"
ps aux | grep -E "orbit/orbit|osqueryd" | grep -v grep | wc -l

echo ""
echo "✓ No symlink errors"
ERRORS=$(journalctl -u orbit.service --since "10 min ago" | grep -c "symlink" || echo "0")
if [ "$ERRORS" -eq 0 ]; then
  echo "✅ No symlink errors"
else
  echo "⚠️  Found symlink errors: $ERRORS"
fi

echo ""
echo "✓ State files present"
ls -la /var/lib/fleetd/orbit.db /var/lib/fleetd/host_identity.crt 2>/dev/null && echo "✅ Files present" || echo "⚠️  Still initializing..."

echo ""
echo "✓ rpm-ostree status"
rpm-ostree status | head -8

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "✅ Installation Complete!"
echo "═══════════════════════════════════════════════════════════"

EOF
```

---

## Step 9: Test in Fleet UI

1. Go to **Fleet** → **Hosts**
2. Find your Silverblue host
3. Should show as enrolled and healthy

### Test Basic Query
```sql
SELECT COUNT(*) as total_rows FROM system_info;
```

### Test Extension (if deployed)
```sql
SELECT id, version, booted, staged, container_image_reference
FROM rpm_ostree_deployments
LIMIT 3;
```

Should return 3 rows (booted, staged, rollback deployments).

---

## Troubleshooting

### Symlink Errors in Logs
```
update failed error="check symlink failed: read existing symlink: readlink /var/lib/fleetd/bin/orbit/orbit: invalid argument"
```

**Cause:** Used `cp -aL` instead of `cp -aP` in Step 3

**Fix:**
```bash
bash /tmp/fix_orbit_symlinks.sh
```

### Orbit Won't Start
```bash
ssh <hostname> "journalctl -u orbit.service -n 50"
```

Common issues:
- `/var/lib/fleetd` doesn't exist → Run Step 3 again
- Permission denied → Check ownership/permissions
- Connection timeout → Network issue to Fleet server

### Extension Not Loading
```bash
ssh <hostname> << 'EOF'
# Check binary exists
ls -la /var/lib/fleetd/extensions/rpm_ostree.ext

# Check flag is present
grep extension /var/lib/fleetd/osquery.flags

# Check logs
journalctl -u orbit.service --since "5 min ago" | grep -i "rpm_ostree\|extension"
EOF
```

---

## Key Differences from Previous Issues

| Issue | Silverblue Install | How Fixed |
|-------|-------------------|-----------|
| Symlink dereferencing | Step 3 uses `-aP` | Preserves symlinks from day 1 |
| Service restart timeout | Uses `deploy-fixed.sh` | Restart runs in background |
| Manual restart after extension | Already handled | Extension auto-loads on restart |

---

## Success Criteria

✅ **Installation is successful when:**

1. Orbit service is active and running
2. No symlink errors in journalctl
3. Fleet shows host as enrolled
4. Osquery queries return results
5. (Optional) Extension returns deployment data

---

## Next Steps

1. ✅ Silverblue is installed and running
2. Create Fleet queries for monitoring
3. Set up automations (if desired)
4. Deploy to additional hosts using this guide

---

## Files & Resources

- **Deployment Script:** `rpm_ostree/deploy-fixed.sh` (improved, works reliably)
- **Original Script:** `rpm_ostree/deploy.sh` (reference)
- **Fix Script:** `/tmp/fix_orbit_symlinks.sh` (if something goes wrong)
- **Extension Docs:** `rpm_ostree/README.md`
- **Extension Tests:** `rpm_ostree/TESTING.md`

---

## Support

If you encounter issues:

1. Check logs: `journalctl -u orbit.service --since "10 min ago"`
2. See [STEP_1_TROUBLESHOOTING.md](STEP_1_TROUBLESHOOTING.md)
3. See [EXTENSION_INSTALL_FIX.md](EXTENSION_INSTALL_FIX.md)

---

**Happy installing! 🚀**
