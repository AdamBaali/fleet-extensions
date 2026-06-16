# Fleet-OSQuery Installation on Jellyfin (Bluefin/rpm-ostree)

## Overview
Deployed fleet-osquery 1.54.0 to an immutable Fedora (Bluefin/rpm-ostree) system, overcoming read-only filesystem constraints.

---

## Step 1: Initial Installation

```bash
sudo rpm-ostree install ./fleet-osquery-1.54.0.aarch64.rpm
systemctl reboot
```

**Outcome**: Package installed as a layered package on top of the immutable base image.

---

## Step 2: Diagnostic Checks

After reboot, verified the orbit service was present but encountered issues:

```bash
# Check if /opt files were relocated
readlink /opt
ls -la /opt/orbit/ 2>/dev/null
ls -la /usr/lib/opt/orbit/ 2>/dev/null
ls -la /var/opt/orbit/ 2>/dev/null

# Verify binary location
ls -la /opt/orbit/bin/orbit/orbit
file /opt/orbit/bin/orbit/orbit

# Check service status
systemctl status orbit.service --no-pager
sudo systemctl start orbit.service
sleep 5
systemctl status orbit.service --no-pager

# Check logs and state files
journalctl -b -u orbit.service --no-pager | tail -50
sudo ls -la /opt/orbit/host_identity.crt /opt/orbit/orbit.db 2>&1
```

**Finding**: Orbit service was crashing due to read-only filesystem at `/opt/orbit` on immutable Fedora.

---

## Step 3: First Workaround Attempt (Bind Mount)

Created a writable copy and attempted bind mount:

```bash
# Stop the restart loop
sudo systemctl stop orbit.service
sudo systemctl disable orbit.service

# Build writable mirror
sudo mkdir -p /var/lib/orbit
sudo cp -aL /opt/orbit/. /var/lib/orbit/

# Create systemd mount unit
sudo tee /etc/systemd/system/opt-orbit.mount > /dev/null <<'EOF'
[Unit]
Description=Writable overlay for /opt/orbit on image-mode Fedora
Before=orbit.service

[Mount]
What=/var/lib/orbit
Where=/opt/orbit
Type=none
Options=bind

[Install]
WantedBy=multi-user.target
EOF

# Activate
sudo systemctl daemon-reload
sudo systemctl enable --now opt-orbit.mount
sudo systemctl enable --now orbit.service
sleep 6
systemctl status orbit.service --no-pager
```

**Result**: Mount unit failed to activate properly. Service still not starting.

**Debugging**:
```bash
# Check why the mount failed
sudo systemctl status opt-orbit.mount --no-pager -l
sudo journalctl -u opt-orbit.mount --no-pager | tail -30

# Check for existing mounts
mount | grep -E '(opt|orbit)'
```

---

## Step 4: Manual Bind Mount Test

Tested bind mount manually to isolate the issue:

```bash
sudo systemctl stop orbit.service
sudo mount --bind /var/lib/orbit /opt/orbit
echo "exit code: $?"
ls -la /opt/orbit/

# Test that orbit binary exists
/opt/orbit/bin/orbit/orbit --help 2>&1 | head -60

# Check service configuration
cat /etc/default/orbit
cat /usr/lib/systemd/system/orbit.service
```

**Result**: Manual mount worked, but systemd unit approach was problematic.

---

## Step 5: Final Solution (CLI Flag Override)

Replaced the bind mount unit with a direct systemd service modification:

```bash
# Tear down the bind-mount workaround
sudo systemctl stop orbit.service
sudo umount /opt/orbit

# Build proper writable state directory
sudo mkdir -p /var/lib/fleetd
sudo cp -aP /opt/orbit/. /var/lib/fleetd/
# NOTE: Use -aP (preserve symlinks) NOT -aL (dereference symlinks)
# Using -aL causes "readlink invalid argument" errors when orbit tries to self-update

# Edit the service to use the new path
sudo systemctl edit orbit.service --force --full
```

**Configuration Change**: Modified orbit.service to use `/var/lib/fleetd` instead of `/opt/orbit` for writable state.

```bash
# Verify the setup
mount | grep orbit
sudo systemctl enable --now orbit.service
sleep 6
systemctl status orbit.service --no-pager
journalctl -b -u orbit.service --no-pager | tail -25

# Confirm state files were created
sudo ls -la /var/lib/fleetd/orbit.db /var/lib/fleetd/host_identity.crt 2>&1
sleep 15
journalctl -b -u orbit.service --no-pager | tail -25

# Verify processes
pgrep -alf orbit
pgrep -alf osquery
```

---

## Step 6: Deployment Confirmation

**Current deployment status** (from rpm-ostree):

| Status | Deployment | Booted | Staged | Fleet Package |
|--------|-----------|--------|--------|---------------|
| Previous | Base Bluefin | ✗ | ✗ | None |
| **Active** | **With fleet-osquery** | **✓** | ✗ | **1.54.0-1.aarch64** |
| **Pending** | **With fleet-osquery** | ✗ | **✓** | **1.54.0-1.aarch64** |

---

## Summary of Challenges & Solutions

### Challenge 1: Read-Only Filesystem
- **Problem**: rpm-ostree creates immutable root; `/opt/orbit` is read-only
- **Solution**: Redirect orbit's writable state to `/var/lib/fleetd` via systemd service configuration

### Challenge 2: Bind Mount Complexity
- **Problem**: Systemd mount units added unnecessary complexity
- **Solution**: Use systemd service edit to override paths directly in the service file

### Challenge 3: Service State Files
- **Problem**: Orbit needs to write `orbit.db` and `host_identity.crt`
- **Solution**: Ensure `/var/lib/fleetd` is writable and has proper permissions

---

## Troubleshooting: Symlink Errors

If you see this error repeating every 15 minutes in `journalctl`:

```
update failed error="check symlink failed: read existing symlink: readlink /var/lib/fleetd/bin/orbit/orbit: invalid argument"
```

**Root cause:** If Step 5 used `cp -aL` (dereference) instead of `cp -aP` (preserve), symlinks were converted to regular files.

**Fix:**
```bash
sudo systemctl stop orbit.service
sudo rm -f /var/lib/fleetd/bin/orbit/orbit
sudo rm -f /var/lib/fleetd/bin/osqueryd/osqueryd
sudo ln -s /opt/orbit/bin/orbit/linux-arm64/stable/orbit /var/lib/fleetd/bin/orbit/orbit
sudo ln -s /opt/orbit/bin/osqueryd/linux-arm64/stable/osqueryd /var/lib/fleetd/bin/osqueryd/osqueryd
sudo systemctl start orbit.service
```

See [STEP_1_TROUBLESHOOTING.md](STEP_1_TROUBLESHOOTING.md) for full details.

---

## Final Notes

- Fleet-osquery successfully deployed and persisted across deployments
- Next deployment (staged) will also include fleet-osquery as a layered package
- The solution is clean and maintainable without additional systemd units
- Both orbit and osquery processes are running and healthy
- **Important:** Use `cp -aP` (not `cp -aL`) to preserve symlinks in Step 5
