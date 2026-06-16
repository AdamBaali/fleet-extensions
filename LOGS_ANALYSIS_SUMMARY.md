# Logs Analysis Summary - Step 1 Issue Found & Fixed

**Date:** June 16, 2026  
**Status:** ✅ **ISSUE IDENTIFIED & SOLUTION PROVIDED**

---

## The Issue Someone Hit at Step 1

When attempting to install fleet-osquery on rpm-ostree (Bluefin), the installation completes but the orbit service encounters persistent errors:

```
2026-06-16T08:33:36+02:00 INF update failed error="check symlink failed: read existing symlink: readlink /var/lib/fleetd/bin/orbit/orbit: invalid argument"
```

This error repeats **every 15 minutes** in journalctl, blocking orbit's self-update mechanism.

---

## Root Cause Analysis

### What Went Wrong

1. **Original structure in `/opt/orbit`:**
   ```bash
   /opt/orbit/bin/orbit/orbit  →  [SYMLINK] → /opt/orbit/bin/orbit/linux-arm64/stable/orbit
   /opt/orbit/bin/osqueryd/osqueryd  →  [SYMLINK] → /opt/orbit/bin/osqueryd/linux-arm64/stable/osqueryd
   ```

2. **Files were copied with `-aL` flag:**
   ```bash
   sudo cp -aL /opt/orbit/. /var/lib/fleetd/
   # -L = dereference symlinks (copy the TARGET file, not the symlink)
   ```

3. **Result in `/var/lib/fleetd`:**
   ```bash
   /var/lib/fleetd/bin/orbit/orbit  →  [REGULAR FILE - 36MB binary]
   /var/lib/fleetd/bin/osqueryd/osqueryd  →  [MISSING - symlink not copied]
   ```

4. **When orbit tries to update:**
   - Orbit checks if `/var/lib/fleetd/bin/orbit/orbit` is a symlink using `readlink()`
   - `readlink()` on a regular file returns "invalid argument"
   - Error is logged but orbit continues (non-blocking)
   - Check repeats every 15 minutes

### Why This Happened

The initial decision to use `cp -aL` was to handle immutable filesystems by copying actual files instead of symlinks. However, this breaks orbit's update mechanism which expects the symlink structure.

---

## The Fix

### For Users Already Affected

Run this command on the affected host:

```bash
ssh bluefin << 'EOF'
set -e
echo "Fixing orbit symlinks..."
sudo systemctl stop orbit.service
sleep 2
sudo rm -f /var/lib/fleetd/bin/orbit/orbit
sudo rm -f /var/lib/fleetd/bin/osqueryd/osqueryd
sudo ln -s /opt/orbit/bin/orbit/linux-arm64/stable/orbit /var/lib/fleetd/bin/orbit/orbit
sudo ln -s /opt/orbit/bin/osqueryd/linux-arm64/stable/osqueryd /var/lib/fleetd/bin/osqueryd/osqueryd
sudo systemctl start orbit.service
sleep 3
echo "✅ Fixed! Checking logs..."
journalctl -u orbit.service --since "2 min ago" | grep -i symlink || echo "No symlink errors found!"
EOF
```

Or use the provided script:
```bash
bash /tmp/fix_orbit_symlinks.sh
```

### For Future Installations

Change **Step 5** in [JELLYFIN_FLEETDM_INSTALL_STEPS.md](JELLYFIN_FLEETDM_INSTALL_STEPS.md):

```bash
# OLD (dereferences symlinks - causes the bug)
sudo cp -aL /opt/orbit/. /var/lib/fleetd/

# NEW (preserves symlinks - correct)
sudo cp -aP /opt/orbit/. /var/lib/fleetd/
```

The `-P` flag preserves symlinks instead of dereferencing them.

---

## Verification

After applying the fix, verify:

```bash
# Check symlinks exist
ssh bluefin 'file /var/lib/fleetd/bin/orbit/orbit'
# Expected: "symlink to /opt/orbit/bin/orbit/linux-arm64/stable/orbit"

# Check for errors gone
ssh bluefin 'journalctl -u orbit.service --since "10 min ago" | grep symlink'
# Expected: [empty - no matches]

# Check service health
ssh bluefin 'systemctl status orbit.service --no-pager | grep Active'
# Expected: "Active: active (running)"
```

---

## Files Updated

1. **[STEP_1_TROUBLESHOOTING.md](STEP_1_TROUBLESHOOTING.md)** - New comprehensive troubleshooting guide
2. **[JELLYFIN_FLEETDM_INSTALL_STEPS.md](JELLYFIN_FLEETDM_INSTALL_STEPS.md)** - Updated with correct flags and warnings
3. **[/tmp/fix_orbit_symlinks.sh](/tmp/fix_orbit_symlinks.sh)** - Automated fix script

---

## Current Host Status (Bluefin)

**After applying the fix, the host will:**
- ✅ Stop showing symlink errors every 15 minutes
- ✅ Allow orbit's self-update mechanism to work
- ✅ Continue running osquery without interruption
- ✅ Maintain all deployed extensions (rpm_ostree_deployments, etc.)

---

## Impact Assessment

| Aspect | Before Fix | After Fix |
|--------|-----------|-----------|
| Orbit running | ✅ Yes | ✅ Yes |
| Osquery working | ✅ Yes | ✅ Yes |
| Self-updates | ❌ Blocked | ✅ Working |
| Log noise | ❌ Every 15 min | ✅ None |
| Extension registration | ✅ Yes | ✅ Yes |
| Fleet queries | ✅ Working | ✅ Working |

---

## Key Lessons

1. **Symlink handling matters** - Orbit internally expects symlink structures
2. **`cp -a` flags are critical** - `-L` vs `-P` has big consequences
3. **Non-blocking errors are still errors** - They silently break features (auto-updates)
4. **Immutable filesystems need special handling** - But not at the cost of compatibility

---

## Next Steps

1. ✅ Apply the fix to bluefin using one of the methods above
2. ✅ Verify no symlink errors appear in journalctl
3. ✅ Document this for any future rpm-ostree deployments
4. ✅ Update deployment scripts to use `cp -aP` instead of `cp -aL`

---

## Questions?

See:
- [STEP_1_TROUBLESHOOTING.md](STEP_1_TROUBLESHOOTING.md) - Detailed troubleshooting
- [JELLYFIN_FLEETDM_INSTALL_STEPS.md](JELLYFIN_FLEETDM_INSTALL_STEPS.md) - Installation guide with fixes
- [DEPLOYMENT_STATUS.md](DEPLOYMENT_STATUS.md) - Overall deployment status
- `/tmp/fix_orbit_symlinks.sh` - Automated fix script
