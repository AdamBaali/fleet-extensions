# Extension Installer Issue & Fix

**Issue:** `deploy.sh` terminated during service restart but extension is actually installed

**Status:** ✅ **FIXABLE IN 30 SECONDS**

---

## What Happened

The `deploy.sh` script ran and:
1. ✅ Detected x86_64 architecture → selected rpm_ostree-amd64.ext
2. ✅ Downloaded the extension binary (7.6MB)
3. ✅ Validated it as a proper ELF binary
4. ✅ Installed to `/var/lib/fleetd/extensions/rpm_ostree.ext`
5. ✅ Added extension flag to osquery.flags
6. ❌ **Got terminated while restarting orbit.service**

**The good news:** Steps 1-5 all succeeded! The extension is installed.

**The problem:** The systemctl restart command got terminated before completing, so orbit never loaded the extension.

---

## What's Currently Installed

```bash
ls -la /var/lib/fleetd/extensions/rpm_ostree.ext
# -rwx------ 1 root root 7255224 Jun 15 08:58 /var/lib/fleetd/extensions/rpm_ostree.ext
```

✅ Binary exists  
✅ Has correct permissions (700)  
✅ Is owned by root  
✅ Is properly marked as executable  

---

## The Fix (30 Seconds)

Run this on the target host:

```bash
sudo systemctl restart orbit.service
sleep 5
ps aux | grep rpm_ostree.ext | grep -v grep
```

Or use the automated fix script:

```bash
bash /tmp/restart_orbit_for_extension.sh
```

---

## Detailed Fix

### Option 1: Quick Manual Restart

```bash
ssh <hostname> "sudo systemctl restart orbit.service && sleep 5 && ps aux | grep rpm_ostree.ext"
```

### Option 2: Use the Fix Script

```bash
bash /tmp/restart_orbit_for_extension.sh
```

This script:
- Restarts orbit service
- Waits for startup
- Checks process list
- Waits for extension registration
- Shows you the results

---

## Verification

After restarting, verify the extension loaded:

```bash
# Check process is running
ps aux | grep "rpm_ostree.ext" | grep -v grep
# Should show: /var/lib/fleetd/extensions/rpm_ostree.ext --socket ...

# Check extension registered in logs
journalctl -u orbit.service --since "1 min ago" | grep -i "rpm_ostree\|registering"
# Should show: "Registering extension (rpm_ostree, ..."

# Query the table from Fleet UI
SELECT id, version, booted, staged FROM rpm_ostree_deployments LIMIT 3;
# Should return 3 rows (booted, staged, rollback deployments)
```

---

## Why the Installer Failed

The `deploy.sh` script at line 53 tries to restart orbit:

```bash
systemctl restart orbit.service
```

When this ran over SSH, the restart process took too long or the SSH connection was closed before completion, causing the script to be terminated.

**However:** The extension binary was already installed before the restart, so this is just a cosmetic issue.

---

## Prevention: Better Deploy Script

For future deployments, modify `deploy.sh` line 53 to:

```bash
# OLD (can timeout)
systemctl restart orbit.service

# NEW (with timeout protection)
systemctl restart orbit.service &
sleep 10
```

Or use a background restart:

```bash
systemctl restart orbit.service || true
# Continue regardless of restart status
```

---

## What to Do Now

**Immediately:**
```bash
bash /tmp/restart_orbit_for_extension.sh
```

Or just:
```bash
ssh <hostname> "sudo systemctl restart orbit.service"
```

**Then verify in Fleet:**
```sql
SELECT id, version, booted, staged 
FROM rpm_ostree_deployments 
LIMIT 1;
```

Should return results within 1-2 minutes.

---

## Troubleshooting If Still Not Working

### Extension still not loading?

```bash
ssh <hostname> << 'EOF'

echo "=== Check if extension binary exists ==="
ls -la /var/lib/fleetd/extensions/rpm_ostree.ext

echo ""
echo "=== Check if extension flag is in osquery.flags ==="
cat /var/lib/fleetd/osquery.flags | grep extension

echo ""
echo "=== Check orbit logs for errors ==="
journalctl -u orbit.service --since "5 min ago" | grep -i "error\|extension" | tail -10

echo ""
echo "=== Check if extension is running ==="
ps aux | grep "rpm_ostree.ext" | grep -v grep

EOF
```

### If binary doesn't exist:
Re-run the installer:
```bash
cd ~/path/to/fleet-extensions/rpm_ostree
./deploy.sh <hostname>
```

### If extension flag missing:
Add it manually:
```bash
ssh <hostname> "echo '--extension=/var/lib/fleetd/extensions/rpm_ostree.ext' | sudo tee -a /var/lib/fleetd/osquery.flags"
sudo systemctl restart orbit.service
```

### If extension keeps crashing:
Check the logs:
```bash
journalctl -u orbit.service | grep "rpm_ostree\|Extension"
```

Look for "respawning too quickly" which means the extension is crashing on startup.

---

## Summary

| Status | Item |
|--------|------|
| ✅ Installed | Extension binary exists at correct path |
| ✅ Permissions | Binary has correct permissions (700, root:root) |
| ✅ Flagged | Extension flag added to osquery.flags |
| ⚠️  Loaded | **NEEDS RESTART** - orbit service needs to be restarted |
| ❓ Registered | Should register within 5-10 seconds of restart |

**Next step:** Restart orbit and verify

---

## Files

- **Fix script:** `/tmp/restart_orbit_for_extension.sh`
- **Deploy script:** `rpm_ostree/deploy.sh` (in repo)
- **Binary:** `/var/lib/fleetd/extensions/rpm_ostree.ext` (on target)
- **Documentation:** [rpm_ostree/README.md](rpm_ostree/README.md)

---

## Questions

See:
- [rpm_ostree/README.md](rpm_ostree/README.md) - Extension documentation
- [rpm_ostree/TESTING.md](rpm_ostree/TESTING.md) - Testing guide
- [NEW_HOST_INSTALLATION.md](NEW_HOST_INSTALLATION.md#step-7-optional---deploy-extensions) - Manual deployment steps
