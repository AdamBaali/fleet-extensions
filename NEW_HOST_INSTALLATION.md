# Complete Installation Guide for rpm-ostree Hosts

**Latest Update:** June 16, 2026  
**Status:** ✅ Tested & Verified

---

## Prerequisites

You're installing on an **rpm-ostree system** (Fedora Silverblue, Bluefin, etc.):

- **OS:** Fedora with rpm-ostree (Silverblue, Bluefin, etc.)
- **Architecture:** arm64 or amd64
- **Fleet Server:** Already enrolled and reachable
- **Access:** SSH root access to the target host

---

## Quick Reference: What Gets Installed

```
1. Fleet-OSQuery 1.54.0 as a layered package
   ├─ Includes orbit daemon and osqueryd
   ├─ Handles read-only filesystem via /var/lib/fleetd writable state
   └─ Persists across system updates

2. rpm_ostree_deployments osquery extension (optional)
   └─ Provides visibility into rpm-ostree deployment status
```

---

## Step 0: Gather Prerequisites

### 1. SSH Access
```bash
# Verify you can reach the host
ssh <hostname> "echo 'Connected successfully'"

# For examples in this guide, we use 'bluefin' alias
# Replace with your actual hostname/IP
```

### 2. Fleet Enrollment Secret
You'll need your Fleet server's enrollment secret:
- Ask your Fleet admin
- Usually found at: Fleet UI → Settings → Organization settings → Enroll secret

### 3. Know Your Architecture
```bash
ssh <hostname> "uname -m"
# Output: aarch64 (= arm64) or x86_64 (= amd64)
```

---

## Step 1: Download Fleet-OSQuery RPM

**Recommended:** Use fleet-osquery 1.54.0 (arm64 or amd64)

```bash
# For arm64 (aarch64)
cd /tmp
curl -L -o fleet-osquery-1.54.0.aarch64.rpm \
  "https://github.com/fleetdm/fleet/releases/download/fleet-osquery-1.54.0/fleet-osquery-1.54.0-1.aarch64.rpm"

# For amd64 (x86_64) 
cd /tmp
curl -L -o fleet-osquery-1.54.0.x86_64.rpm \
  "https://github.com/fleetdm/fleet/releases/download/fleet-osquery-1.54.0/fleet-osquery-1.54.0-1.x86_64.rpm"
```

Verify download:
```bash
ls -lh /tmp/fleet-osquery-1.54.0.*.rpm
```

---

## Step 2: Install as Layered Package

**⚠️ CRITICAL:** Use `rpm-ostree install`, NOT regular `dnf install` (read-only filesystem)

```bash
ssh <hostname> << 'EOF'
set -e

# Transfer and install (this adds fleet-osquery as a layered package)
echo "Installing fleet-osquery as layered package..."
sudo rpm-ostree install /tmp/fleet-osquery-1.54.0.aarch64.rpm

# This REQUIRES a reboot to take effect
echo "Rebooting to apply layered package..."
sudo systemctl reboot
EOF

# Wait for reboot (~2-3 minutes)
echo "Waiting for host to reboot..."
sleep 120

# Verify it's back
ssh <hostname> "echo 'Host is back online'"
```

---

## Step 3: Prepare Writable State Directory

The immutable root filesystem requires a writable location for orbit's state files.

```bash
ssh <hostname> << 'EOF'
set -e

echo "Setting up writable state directory..."

# Create writable directory for orbit state
sudo mkdir -p /var/lib/fleetd
sudo chown root:root /var/lib/fleetd
sudo chmod 755 /var/lib/fleetd

# Copy orbit files from /opt/orbit to /var/lib/fleetd
# ⚠️ CRITICAL: Use -aP (preserve symlinks), NOT -aL (dereference)
echo "Copying orbit files (preserving symlinks)..."
sudo cp -aP /opt/orbit/. /var/lib/fleetd/

# Verify symlinks were preserved (not dereferenced to regular files)
echo "Verifying symlinks..."
file /var/lib/fleetd/bin/orbit/orbit
file /var/lib/fleetd/bin/osqueryd/osqueryd
# Should output "symlink to ..." NOT "ELF 64-bit LSB executable"

EOF
```

Expected output:
```
/var/lib/fleetd/bin/orbit/orbit: symbolic link to /opt/orbit/bin/orbit/linux-arm64/stable/orbit
/var/lib/fleetd/bin/osqueryd/osqueryd: symbolic link to /opt/orbit/bin/osqueryd/linux-arm64/stable/osqueryd
```

**If you see "ELF 64-bit executable" instead of "symbolic link", STOP and run the fix:**
```bash
bash /tmp/fix_orbit_symlinks.sh
```

---

## Step 4: Modify Orbit SystemD Service

The default systemd service uses `/opt/orbit` but we need `/var/lib/fleetd`:

```bash
ssh <hostname> << 'EOF'
set -e

echo "Modifying orbit.service..."

# Edit the service to redirect to /var/lib/fleetd
sudo systemctl edit orbit.service --force --full

# NOTE: This opens your default editor. If using interactive:
# 1. Find the line starting with "ExecStart=/opt/orbit/bin/orbit/orbit"
# 2. Change it to: ExecStart=/opt/orbit/bin/orbit/orbit --root-dir=/var/lib/fleetd
# 3. Save and exit

# If non-interactive, here's the replacement:
CURRENT=$(grep -A 20 "^\[Service\]" /etc/systemd/system/orbit.service)
if echo "$CURRENT" | grep -q "\-\-root-dir=/var/lib/fleetd"; then
  echo "Service already modified, skipping..."
else
  echo "Applying modification..."
  sudo sed -i 's|ExecStart=/opt/orbit/bin/orbit/orbit|ExecStart=/opt/orbit/bin/orbit/orbit --root-dir=/var/lib/fleetd|' \
    /etc/systemd/system/orbit.service
fi

echo "Reloading systemd..."
sudo systemctl daemon-reload

EOF
```

Verify the change:
```bash
ssh <hostname> "sudo grep ExecStart /etc/systemd/system/orbit.service"
# Should output: ExecStart=/opt/orbit/bin/orbit/orbit --root-dir=/var/lib/fleetd
```

---

## Step 5: Start Orbit Service

```bash
ssh <hostname> << 'EOF'
set -e

echo "Starting orbit service..."
sudo systemctl enable orbit.service
sudo systemctl start orbit.service

# Give it time to initialize
sleep 5

# Check status
echo "Checking orbit service status..."
systemctl status orbit.service --no-pager | head -20

# Verify processes are running
echo "Checking for orbit and osquery processes..."
ps aux | grep -E "orbit|osquery" | grep -v grep || echo "No processes found yet (may still be starting)"

EOF
```

Expected processes running:
```
/opt/orbit/bin/orbit/orbit --root-dir=/var/lib/fleetd
/var/lib/fleetd/bin/osqueryd/linux-arm64/stable/osqueryd (with many flags)
/var/lib/fleetd/bin/osqueryd/linux-arm64/stable/osqueryd (monitoring process)
```

---

## Step 6: Verify Fleet Enrollment

```bash
ssh <hostname> << 'EOF'
set -e

echo "Checking orbit logs for enrollment..."

# Look for successful enrollment
journalctl -b -u orbit.service --no-pager | grep -i "enroll\|registered" | tail -10

# Check for symlink errors (if present, run the fix script)
echo "Checking for any errors..."
journalctl -b -u orbit.service --no-pager | grep -i "error\|fatal" | grep -v "authorized_keys\|drop privileges" | tail -5

echo "Done!"

EOF
```

**Expected output:** Should see enrollment success messages, NO symlink errors.

---

## Step 7: Optional - Deploy Extensions

### Deploy rpm_ostree_deployments Extension

This extension provides visibility into rpm-ostree deployments:

```bash
ssh <hostname> << 'EOF'
set -e

echo "Deploying rpm_ostree_deployments extension..."

# Create extension directory if needed
sudo mkdir -p /var/lib/fleetd/extensions

# Copy the compiled extension (arm64 example)
# First, copy from your machine or download it
scp ~/path/to/rpm_ostree-arm64.ext <hostname>:/tmp/
# Then install:
sudo cp /tmp/rpm_ostree-arm64.ext /var/lib/fleetd/extensions/rpm_ostree.ext
sudo chmod 700 /var/lib/fleetd/extensions/rpm_ostree.ext
sudo chown root:root /var/lib/fleetd/extensions/rpm_ostree.ext

# Add to osquery flags
echo "--extension=/var/lib/fleetd/extensions/rpm_ostree.ext" | \
  sudo tee -a /var/lib/fleetd/osquery.flags

# Restart orbit to load the extension
sudo systemctl restart orbit.service
sleep 5

# Verify it registered
echo "Checking for extension registration..."
journalctl -u orbit.service --since "1 min ago" | grep -i "rpm_ostree\|registering" || echo "Check in a moment..."

EOF
```

### Test the Extension

In Fleet UI:
1. Go to **Queries** → **Run query**
2. Select the target host
3. Run this query:
```sql
SELECT id, version, booted, staged, container_image_reference 
FROM rpm_ostree_deployments;
```

Expected: 3 rows (booted, staged, rollback deployments)

---

## Step 8: Verify Everything

```bash
# Run all checks
ssh <hostname> << 'EOF'
set -e

echo "═══════════════════════════════════════════════════════════"
echo "Fleet-OSQuery Installation Verification"
echo "═══════════════════════════════════════════════════════════"

echo ""
echo "✓ Check 1: Symlinks are preserved (not dereferenced)"
echo "---"
file /var/lib/fleetd/bin/orbit/orbit
file /var/lib/fleetd/bin/osqueryd/osqueryd

echo ""
echo "✓ Check 2: orbit service is active"
echo "---"
systemctl is-active orbit.service

echo ""
echo "✓ Check 3: Processes running"
echo "---"
ps aux | grep -E "orbit/orbit|osqueryd" | grep -v grep | wc -l

echo ""
echo "✓ Check 4: No symlink errors in logs"
echo "---"
ERRORS=$(journalctl -u orbit.service --since "10 min ago" | grep -c "symlink" || echo "0")
if [ "$ERRORS" -eq 0 ]; then
  echo "✅ No symlink errors found"
else
  echo "⚠️  Found $ERRORS symlink errors - run fix_orbit_symlinks.sh"
fi

echo ""
echo "✓ Check 5: Orbit has state files"
echo "---"
ls -la /var/lib/fleetd/orbit.db /var/lib/fleetd/host_identity.crt 2>/dev/null && echo "✅ State files present" || echo "⚠️  State files not yet created (may still be initializing)"

echo ""
echo "✓ Check 6: rpm-ostree status"
echo "---"
rpm-ostree status | head -10

echo ""
echo "═══════════════════════════════════════════════════════════"
echo "✅ Installation Complete!"
echo "═══════════════════════════════════════════════════════════"

EOF
```

---

## Troubleshooting

### Problem: Symlink Errors in Logs

```
update failed error="check symlink failed: read existing symlink..."
```

**Cause:** Step 3 used `cp -aL` instead of `cp -aP`

**Fix:**
```bash
bash /tmp/fix_orbit_symlinks.sh
```

Or manually:
```bash
ssh <hostname> << 'EOF'
sudo systemctl stop orbit.service
sudo rm -f /var/lib/fleetd/bin/orbit/orbit /var/lib/fleetd/bin/osqueryd/osqueryd
sudo ln -s /opt/orbit/bin/orbit/linux-arm64/stable/orbit /var/lib/fleetd/bin/orbit/orbit
sudo ln -s /opt/orbit/bin/osqueryd/linux-arm64/stable/osqueryd /var/lib/fleetd/bin/osqueryd/osqueryd
sudo systemctl start orbit.service
EOF
```

### Problem: Orbit Service Won't Start

```bash
ssh <hostname> "journalctl -u orbit.service -n 50 --no-pager"
```

Common issues:
- **"No such file or directory"** → `/var/lib/fleetd` doesn't exist (run Step 3)
- **"Permission denied"** → Wrong ownership/permissions (check `ls -la /var/lib/fleetd`)
- **"read: connection timed out"** → Network issue to Fleet server, wait and retry

### Problem: Extension Not Loading

```bash
ssh <hostname> << 'EOF'
# Check if extension file exists and is executable
ls -la /var/lib/fleetd/extensions/rpm_ostree.ext

# Check if flag is present
grep extension /var/lib/fleetd/osquery.flags

# Check logs
journalctl -u orbit.service --since "5 min ago" | grep -i "extension\|rpm_ostree"
EOF
```

---

## File Checklist

After installation, you should have:

```
✓ /opt/orbit/                             (original from fleet-osquery package)
✓ /var/lib/fleetd/                        (writable copy with symlinks preserved)
  ├─ bin/
  │  ├─ orbit/orbit -> /opt/orbit/bin/orbit/linux-arm64/stable/orbit
  │  └─ osqueryd/osqueryd -> /opt/orbit/bin/osqueryd/linux-arm64/stable/osqueryd
  ├─ osquery.db                            (created by osquery)
  ├─ orbit.db                              (created by orbit)
  ├─ host_identity.crt                     (created by orbit)
  └─ extensions/                           (if installing extensions)
      └─ rpm_ostree.ext

✓ /etc/systemd/system/orbit.service       (modified with --root-dir)
✓ /var/lib/fleetd/osquery.flags           (extension flags added)
```

---

## Verification Queries

Run these in Fleet UI to verify everything works:

```sql
-- Check that osquery is collecting data
SELECT COUNT(*) as total_rows FROM system_info;

-- Check rpm-ostree deployments (if extension installed)
SELECT id, version, booted, staged 
FROM rpm_ostree_deployments 
WHERE booted = 1;

-- Check installed packages
SELECT name, version FROM deb_packages LIMIT 5;
-- (or 'rpm_packages' if using rpm)
```

---

## Next Steps

1. ✅ Host is installed and running
2. Create Fleet queries for monitoring:
   - rpm-ostree deployment tracking
   - Package updates
   - System health
3. Deploy additional extensions as needed
4. Set up Fleet automations (optional)

---

## Support Files

If you encounter issues, see:
- [STEP_1_TROUBLESHOOTING.md](STEP_1_TROUBLESHOOTING.md) - Detailed troubleshooting
- [LOGS_ANALYSIS_SUMMARY.md](LOGS_ANALYSIS_SUMMARY.md) - Technical analysis of common issues
- [DEPLOYMENT_STATUS.md](DEPLOYMENT_STATUS.md) - Example deployment reference
- [/tmp/fix_orbit_symlinks.sh](/tmp/fix_orbit_symlinks.sh) - Automatic symlink fixer

---

## Success Criteria

✅ **Installation is successful when:**
1. Orbit service is active and running
2. No symlink errors in journalctl
3. Fleet server shows the host as enrolled
4. Osquery queries return results
5. (Optional) rpm_ostree_deployments extension returns deployment data
