# Quick Installation Checklist

Use this as a quick reference while following [NEW_HOST_INSTALLATION.md](NEW_HOST_INSTALLATION.md).

---

## Pre-Installation

- [ ] Target host is rpm-ostree system (Silverblue/Bluefin/etc)
- [ ] You have SSH root access to the host
- [ ] You have Fleet enrollment secret
- [ ] You know the host architecture: `ssh <host> "uname -m"`
- [ ] Fleet server is reachable from the target host

---

## Step 1: Download RPM

```bash
# arm64
curl -L -o /tmp/fleet-osquery-1.54.0.aarch64.rpm \
  "https://github.com/fleetdm/fleet/releases/download/fleet-osquery-1.54.0/fleet-osquery-1.54.0-1.aarch64.rpm"

# OR amd64
curl -L -o /tmp/fleet-osquery-1.54.0.x86_64.rpm \
  "https://github.com/fleetdm/fleet/releases/download/fleet-osquery-1.54.0/fleet-osquery-1.54.0-1.x86_64.rpm"
```

- [ ] RPM downloaded to `/tmp/`
- [ ] File verified: `ls -lh /tmp/fleet-osquery-1.54.0.*.rpm`

---

## Step 2: Install & Reboot

```bash
ssh <hostname> "sudo rpm-ostree install /tmp/fleet-osquery-1.54.0.aarch64.rpm && sudo systemctl reboot"
```

- [ ] Installation command ran
- [ ] System reboot initiated
- [ ] Waited 2-3 minutes for reboot
- [ ] SSH reconnect successful: `ssh <hostname> "echo OK"`

---

## Step 3: Copy Files with Symlinks Preserved

```bash
ssh <hostname> << 'EOF'
sudo mkdir -p /var/lib/fleetd
sudo cp -aP /opt/orbit/. /var/lib/fleetd/
file /var/lib/fleetd/bin/orbit/orbit
file /var/lib/fleetd/bin/osqueryd/osqueryd
EOF
```

- [ ] `/var/lib/fleetd/` directory created
- [ ] Files copied with `cp -aP` (✓ PRESERVE symlinks)
- [ ] `file` output shows "symbolic link to" (NOT "ELF 64-bit")
  - If wrong, run: `bash /tmp/fix_orbit_symlinks.sh`

---

## Step 4: Modify orbit.service

```bash
ssh <hostname> "sudo sed -i 's|ExecStart=/opt/orbit/bin/orbit/orbit|ExecStart=/opt/orbit/bin/orbit/orbit --root-dir=/var/lib/fleetd|' /etc/systemd/system/orbit.service && sudo systemctl daemon-reload"
```

Verify:
```bash
ssh <hostname> "grep ExecStart /etc/systemd/system/orbit.service"
```

- [ ] Service file modified
- [ ] Verify output includes `--root-dir=/var/lib/fleetd`

---

## Step 5: Start Services

```bash
ssh <hostname> << 'EOF'
sudo systemctl enable orbit.service
sudo systemctl start orbit.service
sleep 5
systemctl status orbit.service --no-pager | head -10
EOF
```

- [ ] Service enabled
- [ ] Service started
- [ ] Status shows "Active: active (running)"

---

## Step 6: Check for Errors

```bash
ssh <hostname> "journalctl -u orbit.service --since '10 min ago' | grep -i symlink"
```

Result should be **empty** (no symlink errors).

- [ ] No symlink errors in logs
  - If errors present, run: `bash /tmp/fix_orbit_symlinks.sh`

---

## Step 7: Verify in Fleet UI

1. Go to Fleet → Hosts
2. Find your host - should show as enrolled
3. Host details should show osquery queries running

- [ ] Host appears in Fleet
- [ ] Host shows as enrolled and healthy

---

## Optional: Deploy Extensions

```bash
# Copy extension binary to host
scp ./rpm_ostree/rpm_ostree-arm64.ext <hostname>:/tmp/

# Install on host
ssh <hostname> << 'EOF'
sudo cp /tmp/rpm_ostree-arm64.ext /var/lib/fleetd/extensions/rpm_ostree.ext
sudo chmod 700 /var/lib/fleetd/extensions/rpm_ostree.ext
echo "--extension=/var/lib/fleetd/extensions/rpm_ostree.ext" | sudo tee -a /var/lib/fleetd/osquery.flags
sudo systemctl restart orbit.service
sleep 5
journalctl -u orbit.service --since '1 min ago' | grep rpm_ostree
EOF
```

- [ ] Extension binary copied
- [ ] Extension installed and permissions set
- [ ] Extension flag added to osquery.flags
- [ ] orbit restarted
- [ ] Extension registration message in logs

---

## Final Verification

```bash
ssh <hostname> << 'EOF'
echo "=== Symlinks (should be symbolic link) ==="
file /var/lib/fleetd/bin/orbit/orbit

echo ""
echo "=== Service Status (should be active) ==="
systemctl is-active orbit.service

echo ""
echo "=== Processes (should have 3+ orbit/osquery) ==="
ps aux | grep -E "orbit|osquery" | grep -v grep | wc -l

echo ""
echo "=== Symlink Errors (should be empty) ==="
journalctl -u orbit.service --since "15 min ago" | grep -c symlink || echo "0"

echo ""
echo "=== State Files (should exist) ==="
ls -la /var/lib/fleetd/orbit.db /var/lib/fleetd/host_identity.crt 2>/dev/null && echo "✅" || echo "⚠️  (may still be initializing)"
EOF
```

- [ ] All checks pass

---

## Success! 🎉

Your host is installed and ready. Next:
1. Create Fleet queries for the host
2. Monitor rpm-ostree deployments (if extension installed)
3. Set up automations in Fleet UI

---

## If Something Goes Wrong

| Problem | Solution |
|---------|----------|
| Symlink errors in logs | Run: `bash /tmp/fix_orbit_symlinks.sh` |
| Service won't start | Run: `ssh <host> "journalctl -u orbit.service -n 50"` and check the error |
| Extension not loading | Check: `ssh <host> "grep extension /var/lib/fleetd/osquery.flags"` |
| Host not in Fleet | Check network connectivity and enrollment secret |

See [STEP_1_TROUBLESHOOTING.md](STEP_1_TROUBLESHOOTING.md) for detailed troubleshooting.

---

## Reference Documents

- **Full Guide:** [NEW_HOST_INSTALLATION.md](NEW_HOST_INSTALLATION.md)
- **Troubleshooting:** [STEP_1_TROUBLESHOOTING.md](STEP_1_TROUBLESHOOTING.md)
- **Technical Details:** [LOGS_ANALYSIS_SUMMARY.md](LOGS_ANALYSIS_SUMMARY.md)
- **Fix Script:** [/tmp/fix_orbit_symlinks.sh](/tmp/fix_orbit_symlinks.sh)
