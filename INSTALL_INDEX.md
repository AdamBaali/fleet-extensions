# Fleet-OSQuery Installation Documentation Index

**Last Updated:** June 16, 2026  
**Status:** ✅ Verified & Ready for Production

---

## 🚀 Quick Start

**New to installing fleet-osquery on rpm-ostree?**

1. **5-minute overview:** [QUICK_INSTALL_CHECKLIST.md](QUICK_INSTALL_CHECKLIST.md)
2. **Full guide:** [NEW_HOST_INSTALLATION.md](NEW_HOST_INSTALLATION.md)
3. **Hit a problem?** [STEP_1_TROUBLESHOOTING.md](STEP_1_TROUBLESHOOTING.md)

---

## 📚 Complete Documentation Guide

### For Installing on a New Host

| Document | Purpose | When to Use |
|----------|---------|------------|
| [NEW_HOST_INSTALLATION.md](NEW_HOST_INSTALLATION.md) | Complete step-by-step installation guide | You're installing on a brand new rpm-ostree host |
| [QUICK_INSTALL_CHECKLIST.md](QUICK_INSTALL_CHECKLIST.md) | Quick reference checklist | You've done this before and just need a reminder |
| [STEP_1_TROUBLESHOOTING.md](STEP_1_TROUBLESHOOTING.md) | Detailed troubleshooting guide | Something goes wrong during installation |

### For Understanding the Setup

| Document | Purpose | When to Use |
|----------|---------|------------|
| [LOGS_ANALYSIS_SUMMARY.md](LOGS_ANALYSIS_SUMMARY.md) | Technical deep-dive into the symlink issue and fix | You want to understand WHY things work this way |
| [DEPLOYMENT_STATUS.md](DEPLOYMENT_STATUS.md) | Reference deployment example (bluefin host) | You want to see an example of a successful deployment |
| [JELLYFIN_FLEETDM_INSTALL_STEPS.md](JELLYFIN_FLEETDM_INSTALL_STEPS.md) | Historical reference - earlier installation attempt | Understanding the evolution of the solution |

### For Extension Deployments

| Document | Purpose | When to Use |
|----------|---------|------------|
| [rpm_ostree/README.md](rpm_ostree/README.md) | rpm_ostree_deployments extension documentation | Deploying the rpm-ostree visibility extension |
| [rpm_ostree/TESTING.md](rpm_ostree/TESTING.md) | Extension testing guide | Verifying the extension works correctly |

### Automation Scripts

| Script | Purpose | How to Use |
|--------|---------|-----------|
| [/tmp/fix_orbit_symlinks.sh](/tmp/fix_orbit_symlinks.sh) | Automatic symlink fixer | `bash /tmp/fix_orbit_symlinks.sh` |

---

## 🎯 Common Scenarios

### Scenario 1: Installing on a Brand New Host

**What to do:**
1. Read: [NEW_HOST_INSTALLATION.md](NEW_HOST_INSTALLATION.md)
2. Follow all steps: 0-8
3. Use [QUICK_INSTALL_CHECKLIST.md](QUICK_INSTALL_CHECKLIST.md) as a reference

**Time required:** 15-20 minutes

---

### Scenario 2: "Someone Can't Get Past Step 1"

**What to do:**
1. First, read: [STEP_1_TROUBLESHOOTING.md](STEP_1_TROUBLESHOOTING.md)
2. Check for symlink errors: `journalctl -u orbit.service | grep symlink`
3. If symlink errors exist, run: `bash /tmp/fix_orbit_symlinks.sh`
4. Verify with: [NEW_HOST_INSTALLATION.md - Step 8](NEW_HOST_INSTALLATION.md#step-8-verify-everything)

**Root cause:** Files copied with `cp -aL` (dereferences symlinks) instead of `cp -aP` (preserves symlinks)

---

### Scenario 3: Troubleshooting Failed Installation

**What to do:**
1. Check the logs:
   ```bash
   ssh <hostname> "journalctl -u orbit.service --since '30 min ago' | tail -100"
   ```
2. Match your error to [STEP_1_TROUBLESHOOTING.md](STEP_1_TROUBLESHOOTING.md#troubleshooting)
3. Run the appropriate fix

**Common issues:**
- Symlink errors → `bash /tmp/fix_orbit_symlinks.sh`
- Service won't start → Check `/var/lib/fleetd` exists and has proper permissions
- Extension not loading → Check extension path and osquery.flags

---

### Scenario 4: Installing Extensions

**What to do:**
1. Complete basic installation (Scenarios 1-3 above)
2. Follow [NEW_HOST_INSTALLATION.md - Step 7](NEW_HOST_INSTALLATION.md#step-7-optional---deploy-extensions)
3. Verify with provided Fleet UI queries

---

## 🔑 Key Concepts

### Why /var/lib/fleetd?

On rpm-ostree, the root filesystem is immutable. Orbit needs to write:
- `orbit.db` (state database)
- `host_identity.crt` (enrollment certificate)
- Various cache files

Solution: Keep executable binaries in `/opt/orbit` (from RPM), but redirect writable state to `/var/lib/fleetd`.

### Why cp -aP NOT cp -aL?

- **`cp -aL`** = "Dereference symlinks" → converts symlinks to regular files ❌
- **`cp -aP`** = "Preserve symlinks" → keeps symlink structure intact ✅

Orbit internally expects symlinks (for self-updates), so using `-aL` breaks the update mechanism.

### The --root-dir Flag

Added to orbit service:
```
ExecStart=/opt/orbit/bin/orbit/orbit --root-dir=/var/lib/fleetd
```

Tells orbit: "Keep your writable state in `/var/lib/fleetd` instead of `/opt/orbit`"

---

## 🔍 Reference Data

### Tested Configuration

| Component | Version | Status |
|-----------|---------|--------|
| Fleet-OSQuery | 1.54.0 | ✅ Tested |
| rpm-ostree host | Bluefin LTS | ✅ Tested |
| Architecture | arm64 | ✅ Tested |
| Architecture | amd64 | ✅ Expected to work |
| Extension | rpm_ostree_deployments | ✅ Tested |

### File Structure After Installation

```
/opt/orbit/                          # Original RPM location (read-only)
├─ bin/orbit/linux-arm64/stable/orbit     # Actual binary
└─ bin/osqueryd/linux-arm64/stable/osqueryd

/var/lib/fleetd/                    # Writable copy (our setup)
├─ bin/orbit/orbit                      # Symlink → /opt/orbit/bin/orbit/linux-arm64/stable/orbit
├─ bin/osqueryd/osqueryd                # Symlink → /opt/orbit/bin/osqueryd/linux-arm64/stable/osqueryd
├─ orbit.db                         # Created by orbit
├─ host_identity.crt                # Created by orbit
├─ osquery.db                       # Created by osquery
├─ osquery.flags                    # Extension configurations
└─ extensions/                      # Custom extensions
    └─ rpm_ostree.ext               # Optional

/etc/systemd/system/orbit.service   # Modified to use --root-dir=/var/lib/fleetd
```

---

## 🛠️ Tools & Scripts

### fix_orbit_symlinks.sh

**Purpose:** Fix the symlink issue if it occurs

**Location:** `/tmp/fix_orbit_symlinks.sh`

**Usage:**
```bash
bash /tmp/fix_orbit_symlinks.sh
```

**What it does:**
1. Stops orbit service
2. Removes incorrectly dereferenced files
3. Creates proper symlinks
4. Restarts orbit service
5. Verifies no symlink errors

**When to use:**
- Error: `update failed error="check symlink failed: read existing symlink..."`
- Every 15 minutes in logs

---

## 📋 Installation Workflow

```
START
  ↓
[Are you installing on a new host?]
  ├─ YES → Read: NEW_HOST_INSTALLATION.md → Follow Steps 0-8
  ├─ NO  → Do you have a specific problem?
           ├─ YES → Read: STEP_1_TROUBLESHOOTING.md → Find your error
           └─ NO  → Read: QUICK_INSTALL_CHECKLIST.md → Reference only
  ↓
[Installation Complete]
  ├─ Optional: Deploy extensions (Step 7 in NEW_HOST_INSTALLATION.md)
  ├─ Set up Fleet queries
  └─ Monitor deployment
```

---

## ❓ FAQ

**Q: What if I already used `cp -aL`?**  
A: Run `bash /tmp/fix_orbit_symlinks.sh` to fix it. The system keeps running while you apply the fix.

**Q: Do I need to redeploy the entire system?**  
A: No. The fix is non-destructive and can be applied to a running system.

**Q: What's the difference between aarch64 and arm64?**  
A: Same thing. `aarch64` is the Linux/RPM name, `arm64` is the industry name.

**Q: Can I install this on amd64 (x86_64)?**  
A: Yes, just download the x86_64 RPM and follow the same steps.

**Q: What if I don't want the rpm_ostree extension?**  
A: That's fine. Complete installation without Step 7. The core fleet-osquery works without it.

**Q: How do I uninstall fleet-osquery?**  
A: Use `rpm-ostree uninstall fleet-osquery` and reboot. The `/var/lib/fleetd` directory remains (safe to delete manually if needed).

---

## 🚨 Critical Points

1. **Always use `cp -aP` NOT `cp -aL`** when copying `/opt/orbit` to `/var/lib/fleetd`
2. **Add `--root-dir=/var/lib/fleetd` to ExecStart** in orbit.service
3. **Verify symlinks** after copying: `file /var/lib/fleetd/bin/orbit/orbit` should show "symbolic link"
4. **Check for symlink errors** in logs: `journalctl -u orbit.service | grep symlink`

---

## 📞 Support

**Documentation Issues?**
- See [STEP_1_TROUBLESHOOTING.md](STEP_1_TROUBLESHOOTING.md)

**Still stuck?**
1. Gather logs: `ssh <host> "journalctl -u orbit.service -n 100"` 
2. Check the service: `ssh <host> "systemctl status orbit.service"`
3. Verify files: `ssh <host> "ls -la /var/lib/fleetd/"`

---

## 📝 Document Relationships

```
INSTALL_INDEX.md (YOU ARE HERE)
    ├─ NEW_HOST_INSTALLATION.md (main guide)
    ├─ QUICK_INSTALL_CHECKLIST.md (summary)
    ├─ STEP_1_TROUBLESHOOTING.md (problems)
    ├─ LOGS_ANALYSIS_SUMMARY.md (technical)
    ├─ DEPLOYMENT_STATUS.md (reference)
    ├─ JELLYFIN_FLEETDM_INSTALL_STEPS.md (history)
    └─ rpm_ostree/README.md (extensions)
```

---

## ✅ Verification Checklist

After following the installation:
- [ ] Host appears in Fleet UI
- [ ] Host shows as enrolled
- [ ] Osquery queries return results
- [ ] No symlink errors in journalctl
- [ ] orbit.service shows "active (running)"
- [ ] /var/lib/fleetd/bin/orbit/orbit is a symlink (not a regular file)

---

**Last Updated:** June 16, 2026  
**Tested:** arm64 (Bluefin LTS), expected to work on amd64  
**Status:** ✅ Production Ready
