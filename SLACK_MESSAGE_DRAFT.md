# Slack Message Draft

## Option 1: Short & Punchy

```
🚀 Successfully deployed fleet-osquery 1.54.0 to Bluefin (rpm-ostree immutable Fedora)

The challenge: immutable root filesystems block traditional agent deployments. The solution: layered packages + systemd service path override to redirect orbit's state to a writable directory.

Full deployment details: [link to JELLYFIN_FLEETDM_INSTALL_STEPS.md]
```

---

## Option 2: More Context (Thread-friendly)

```
🚀 Fleet-OSQuery now running on immutable Fedora systems (Bluefin/rpm-ostree)

We just deployed fleet-osquery 1.54.0 to a Bluefin system (immutable Fedora with rpm-ostree). Here's what made it work:

**The Problem:**
• rpm-ostree creates an immutable root filesystem
• Traditional agents expect writable `/opt` directories
• Orbit service would crash trying to write state files

**The Solution:**
• Used rpm-ostree's layering feature to add fleet-osquery as a persistent package layer
• Modified the orbit systemd service to redirect writable state to `/var/lib/fleetd`
• Clean, maintainable approach without extra mount units

**Result:**
✓ Orbit service running
✓ OSQuery collecting data
✓ Persists across system updates/reboots
✓ Ready for next deployment generation

More details: [link to JELLYFIN_FLEETDM_INSTALL_STEPS.md]
```

---

## Option 3: For a Technical Audience

```
📋 rpm-ostree Deployment Pattern for Fleet-OSQuery

Successfully deployed fleet-osquery-1.54.0.aarch64 to Bluefin (immutable Fedora) by solving the read-only filesystem constraint.

**Deployment Approach:**
1. `rpm-ostree install ./fleet-osquery-1.54.0.aarch64.rpm` — layered package installation
2. Systemd service modification to override state directory: `/var/lib/fleetd`
3. Verified orbit + osquery processes running post-reboot

**Key Pattern:**
For immutable container/image-based systems, agent deployment requires:
- Stateless package layers (rpm-ostree/ostree)
- Writable overlay directories for runtime state
- Systemd service config to bridge the gap

**Status:**
- Current deployment: Running ✓
- Staged deployment: Ready (includes fleet-osquery layer) ✓
- Rollback capability: Available ✓

See: [link to JELLYFIN_FLEETDM_INSTALL_STEPS.md]
```

---

## Who to tag:
- @jon (obviously!)
- @fleet-team
- @infra (if they're interested in patterns)
