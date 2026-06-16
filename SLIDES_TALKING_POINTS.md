# Fleet-OSQuery on Immutable Fedora: Slide Talking Points

## Slide 1: The Challenge
**Title: "Deploying Agents to Immutable Systems"**

### Talking Points:
- Modern systems (Fedora CoreOS, Bluefin, uBlue) use immutable root filesystems
- Read-only by design = better security, reproducibility, automatic rollbacks
- But traditional agents expect writable `/opt` directories for configuration and state
- Fleet-osquery's orbit agent needs to write:
  - `orbit.db` (state database)
  - `host_identity.crt` (host identity certificate)
  - Other runtime state

**Problem Statement:** "How do we deploy agents to systems designed to reject changes?"

---

## Slide 2: Our Solution Architecture
**Title: "Layering + Path Override"**

### Visual to show:
```
Base Bluefin Image (Read-Only)
    ↓
rpm-ostree Layered Package (fleet-osquery-1.54.0)
    ↓
Systemd Service Config Override
    ↓
State → /var/lib/fleetd (Writable)
```

### Talking Points:
- **Step 1**: Use rpm-ostree's layering feature to add fleet-osquery as a persistent package layer
- **Step 2**: Modify the orbit systemd service to redirect state writes to `/var/lib/fleetd`
- **Step 3**: `/var/lib/fleetd` is writable on immutable systems (not part of the locked root)
- Clean separation: immutable code, mutable state

---

## Slide 3: Deployment Status
**Title: "Deployment Progression"**

### Show a table:
| Generation | Status | Fleet Package |
|-----------|--------|---------------|
| Previous | — | None |
| **Current (Booted)** | **Active ✓** | **1.54.0** |
| **Next (Staged)** | **Ready to deploy** | **1.54.0** |

### Talking Points:
- Fleet-osquery persists across deployment generations
- Staged deployment is already prepared for next boot
- No manual intervention needed for updates
- Automatic rollback capability if needed

---

## Slide 4: What We Learned (Key Takeaways)
**Title: "Patterns for Immutable Systems"**

### Talking Points:

1. **Layering Over Replacement**
   - Don't try to modify the immutable root
   - Use package layering for persistent additions

2. **Writable Overlays for State**
   - Keep code immutable, state writable
   - Leverage `/var` or `/opt` outside the locked root

3. **Systemd Service Config**
   - Use service overrides instead of complex mount units
   - `systemctl edit` is your friend

4. **Persistence Through Updates**
   - Layered packages survive OS updates
   - Your agent setup is reproducible

### Bridge to Future:
"This pattern works for any agent deployment on immutable systems — not just Fleet."

---

## Slide 5: Technical Details (Optional Deep Dive)
**Title: "The Nuts & Bolts"**

### Key Commands:
```bash
# Installation
sudo rpm-ostree install ./fleet-osquery-1.54.0.aarch64.rpm

# State directory
sudo mkdir -p /var/lib/fleetd
sudo cp -aL /opt/orbit/. /var/lib/fleetd/

# Service override
sudo systemctl edit orbit.service --force --full
```

### Verification:
```bash
systemctl status orbit.service
pgrep -alf orbit
pgrep -alf osquery
```

### Talking Points:
- Orbit and osquery processes both healthy
- State files created and writable
- Service auto-starts on boot
- Logs visible via journalctl

---

## Slide 6: Why This Matters
**Title: "The Broader Impact"**

### Talking Points:
- **Reproducibility**: Same image, same agent state every time
- **Security**: Immutable base reduces attack surface
- **Simplicity**: No manual agent updates per machine
- **Reliability**: Failed agent config doesn't break the OS
- **Scalability**: Pattern scales to fleet-wide deployments

### Closing:
"We've unlocked fleet-osquery for a growing class of modern, immutable operating systems."

---

## Optional Q&A Prep

**Q: Why not just use traditional Linux?**
A: Immutable systems offer better security, automatic rollbacks, and reproducibility. They're the future for edge/embedded/container workloads.

**Q: Will this work on CoreOS, Fedora IoT, etc.?**
A: Yes — any rpm-ostree based system with the same immutable design pattern.

**Q: Can I roll back if the agent breaks?**
A: Yes, rpm-ostree keeps previous deployments. One command to revert.

**Q: Is the state file safe in `/var/lib/fleetd`?**
A: Yes, `/var` persists across OS updates and is designed for application state.

---

## Narrative Arc (for live presentation):

1. **Setup**: "We're trying to deploy Fleet to modern immutable systems"
2. **Conflict**: "But they won't let us write to `/opt`"
3. **Solution**: "So we layered the package and redirected the state"
4. **Resolution**: "And now orbit runs happily on the immutable OS"
5. **Payoff**: "This unlocks Fleet for a whole category of systems"
