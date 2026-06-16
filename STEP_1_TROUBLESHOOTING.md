# Step 1 Troubleshooting - Fleet-OSQuery Installation on rpm-ostree

## Problem

When trying to install fleet-osquery on rpm-ostree (Bluefin/Silverblue), the installation may appear to work initially, but the orbit service encounters recurring errors:

```
update failed error="check symlink failed: read existing symlink: readlink /var/lib/fleetd/bin/orbit/orbit: invalid argument"
```

This error repeats every 15 minutes in the logs, blocking orbit's self-update mechanism.

---

## Root Cause

The issue stems from how files are copied from `/opt/orbit` to `/var/lib/fleetd` during setup.

**In `/opt/orbit/bin/orbit/`:**
```bash
lrwxrwxrwx orbit -> /opt/orbit/bin/orbit/linux-arm64/stable/orbit  # SYMLINK
```

**When copied with `cp -aL` (dereferences symlinks):**
- The `-L` flag converts symlinks to regular files
- Result: `/var/lib/fleetd/bin/orbit/orbit` becomes a regular executable file instead of a symlink

**When orbit tries to update:**
1. Orbit checks if `/var/lib/fleetd/bin/orbit/orbit` is a symlink using `readlink()`
2. Since it's now a regular file, `readlink()` returns "invalid argument"
3. Orbit logs the error but continues running (non-fatal)
4. The error repeats every 15 minutes when orbit checks for updates

---

## Solution

Recreate the proper symlink structure in `/var/lib/fleetd` to match `/opt/orbit`.

### Quick Fix (SSH)

```bash
# Run this entire block as one command block
ssh bluefin << 'EOF'
set -e
echo "Fixing orbit symlinks..."

# Stop the service
sudo systemctl stop orbit.service
sleep 2

# Remove the regular files that should be symlinks
sudo rm -f /var/lib/fleetd/bin/orbit/orbit
sudo rm -f /var/lib/fleetd/bin/osqueryd/osqueryd

# Create proper symlinks pointing to /opt/orbit
sudo ln -s /opt/orbit/bin/orbit/linux-arm64/stable/orbit /var/lib/fleetd/bin/orbit/orbit
sudo ln -s /opt/orbit/bin/osqueryd/linux-arm64/stable/osqueryd /var/lib/fleetd/bin/osqueryd/osqueryd

echo "Symlinks created:"
ls -la /var/lib/fleetd/bin/orbit/orbit
ls -la /var/lib/fleetd/bin/osqueryd/osqueryd

# Restart service
sudo systemctl start orbit.service
sleep 3

echo "✅ Done! Service restarted."
echo "Checking logs for symlink errors..."
journalctl -u orbit.service --since "2 min ago" | grep -i symlink || echo "No symlink errors found!"
EOF
```

### Or Run Script Locally

If you have the fix script from this repo:

```bash
bash /tmp/fix_orbit_symlinks.sh
```

---

## Why This Happens

The initial setup in [JELLYFIN_FLEETDM_INSTALL_STEPS.md](JELLYFIN_FLEETDM_INSTALL_STEPS.md) uses:

```bash
sudo cp -aL /opt/orbit/. /var/lib/fleetd/
```

The `-aL` flags preserve attributes but **dereference symlinks**. This was necessary at the time to copy files to a writable location on an immutable filesystem, but it loses the symlink structure that orbit expects.

---

## What to Do Before Step 1 (Preventive)

When first copying orbit files from `/opt/orbit` to `/var/lib/fleetd`, use **`-P` (no dereference)** instead of `-L`:

```bash
# WRONG - dereferences symlinks
sudo cp -aL /opt/orbit/. /var/lib/fleetd/

# CORRECT - preserves symlinks
sudo cp -aP /opt/orbit/. /var/lib/fleetd/
```

Then skip the symlink recreation step below.

---

## Verification

After applying the fix, verify that:

1. **Symlinks exist** (not regular files):
   ```bash
   ssh bluefin 'file /var/lib/fleetd/bin/orbit/orbit'
   # Should output: "symlink to /opt/orbit/bin/orbit/linux-arm64/stable/orbit"
   
   ssh bluefin 'file /var/lib/fleetd/bin/osqueryd/osqueryd'
   # Should output: "symlink to /opt/orbit/bin/osqueryd/linux-arm64/stable/osqueryd"
   ```

2. **No symlink errors in logs**:
   ```bash
   ssh bluefin 'journalctl -u orbit.service --since "10 min ago" | grep symlink'
   # Should return nothing (no results)
   ```

3. **Orbit service is healthy**:
   ```bash
   ssh bluefin 'systemctl status orbit.service --no-pager | head -10'
   # Should show: "Active: active (running)"
   ```

---

## Impact of This Bug

- **Severity:** Low (orbit continues running)
- **Impact:** Orbit's automatic self-update mechanism is blocked, but all osquery functionality works
- **User Experience:** Every 15 minutes, a harmless error appears in logs
- **Next Steps:** Once symlinks are fixed, orbit can update itself normally

---

## If You're Starting Fresh

Follow [JELLYFIN_FLEETDM_INSTALL_STEPS.md](JELLYFIN_FLEETDM_INSTALL_STEPS.md) normally, but when copying files use:

```bash
# Step: Copy orbit files (modified to preserve symlinks)
sudo mkdir -p /var/lib/fleetd
sudo cp -aP /opt/orbit/. /var/lib/fleetd/
# ^ Use -aP instead of -aL
```

Then continue with the systemd service configuration as documented.

---

## If You Hit This After Installation

Just run the fix script above. The system will keep working during the fix.

---

## Related Issues

- **File:** [JELLYFIN_FLEETDM_INSTALL_STEPS.md](JELLYFIN_FLEETDM_INSTALL_STEPS.md) - Initial installation guide
- **File:** [DEPLOYMENT_STATUS.md](DEPLOYMENT_STATUS.md) - Overall deployment status
- **Symlink Check Error Location:** Orbit's internal update checker around every 15-minute interval
