#!/bin/bash
#
# Installs and loads the rpm_ostree_deployments osquery extension on an
# rpm-ostree based atomic host (Fedora Silverblue/Kinoite, Universal Blue /
# Bluefin, Fedora CoreOS, ...) running Fleet's orbit.
#
# Designed for Fleet script execution: idempotent, structured "key : value"
# output, numbered exit codes, payload + architecture validation, and a
# self-verifying tail that prints the Fleet live queries to confirm the table
# is active. Rerunning re-asserts the binary, loader, perms, and orbit config.
#
# Binaries are produced by CI on push to main and attached to the `latest`
# release, so this script always pulls from there; no version needs bumping.
#
# Image-mode/atomic layout. On image-mode systems (Bluefin/Silverblue/CoreOS)
# fleetd is rpm-ostree *layered*, so /usr and orbit's default root /opt/orbit
# are read-only, while /etc and /var are writable. So:
#   - the binary + loader live under /var/lib/fleetd (writable), and
#   - orbit is pointed at them with ORBIT_ROOT_DIR and
#     ORBIT_OSQUERY_EXTENSIONS_AUTOLOAD set via a systemd drop-in under
#     /etc/systemd/system/orbit.service.d/. The drop-in is the canonical
#     systemd override and, unlike an edit to the package-managed
#     /etc/default/orbit (which a fleetd upgrade overwrites, fleetdm/fleet
#     #18365), it survives package/image updates. We also mirror the two vars
#     into /etc/default/orbit for compatibility with the documented fix.
# osquery refuses an extension that is not root-owned or is world-writable, so
# the binary is installed root:root mode 0700.
#
# Exit codes:
#   0 = Installed; orbit back to active
#   2 = Not run as root
#   3 = orbit.service not present (is fleetd installed?)
#   4 = Filesystem/config operation failed
#   5 = orbit did not return to active after restart
#   6 = Download failed or asset is not a valid ELF for this architecture
#   7 = rpm-ostree not found (not an atomic host)
#   8 = Unsupported architecture
#
# Usage:
#   sudo ./install-rpm-ostree-extension.sh

set -euo pipefail

# ============================================================================
# Extension identity.
# ============================================================================
EXTENSION_NAME="rpm_ostree"
GITHUB_REPO="allenhouchins/fleet-extensions"
BASE_URL="https://github.com/$GITHUB_REPO/releases/latest/download"
# ============================================================================

ORBIT_ROOT_DIR="/var/lib/fleetd"
EXTENSION_DIR="$ORBIT_ROOT_DIR/extensions"
EXTENSION_PATH="$EXTENSION_DIR/$EXTENSION_NAME.ext"   # .ext suffix required by osquery
AUTOLOAD_FILE="$ORBIT_ROOT_DIR/extensions.load"
ORBIT_DEFAULTS="/etc/default/orbit"
ORBIT_DROPIN_DIR="/etc/systemd/system/orbit.service.d"
ORBIT_DROPIN="$ORBIT_DROPIN_DIR/10-fleet-extensions.conf"
SERVICE_NAME="orbit.service"
BACKUP_PATH="$EXTENSION_PATH.backup.$(date +%Y%m%d_%H%M%S)"

write_state() { printf '%-32s : %s\n' "$1" "$2"; }
fail()        { echo "FAIL: $1"; write_state "State" "${2:-failed}"; exit "${3:-1}"; }

echo "=== $EXTENSION_NAME extension installer ==="
echo ""

# --- Pre-flight: root, atomic host, orbit present ---------------------------
[[ $EUID -eq 0 ]] || fail "must run as root (use sudo)" "not_root" 2

if ! command -v rpm-ostree &>/dev/null && [[ ! -x /usr/bin/rpm-ostree ]]; then
    fail "rpm-ostree not found; this extension targets atomic/image-mode hosts" "not_atomic" 7
fi

if ! systemctl list-unit-files "$SERVICE_NAME" &>/dev/null && \
   ! systemctl status "$SERVICE_NAME" &>/dev/null; then
    fail "$SERVICE_NAME not found; is fleetd installed?" "service_missing" 3
fi

command -v curl &>/dev/null || fail "curl is required but not found" "no_curl" 4

# --- Architecture selection -------------------------------------------------
arch="$(uname -m)"
case "$arch" in
    x86_64)          ASSET="$EXTENSION_NAME-amd64.ext"; ELF_MACHINE="3e00" ;;  # EM_X86_64
    aarch64|arm64)   ASSET="$EXTENSION_NAME-arm64.ext"; ELF_MACHINE="b700" ;;  # EM_AARCH64
    *)               fail "unsupported architecture: $arch" "unsupported_arch" 8 ;;
esac
URL="$BASE_URL/$ASSET"

write_state "Architecture"    "$arch"
write_state "Asset"           "$ASSET"
write_state "Target"          "$EXTENSION_PATH"
write_state "Loader"          "$AUTOLOAD_FILE"
write_state "orbit defaults"  "$ORBIT_DEFAULTS"
write_state "Download URL"    "$URL"

# --- Ensure the extensions dir exists, owned root:root 0755 -----------------
if ! mkdir -p "$EXTENSION_DIR"; then
    fail "could not create $EXTENSION_DIR" "dir_unwritable" 4
fi
chown root:root "$EXTENSION_DIR" && chmod 0755 "$EXTENSION_DIR" \
    || fail "could not set perms on $EXTENSION_DIR" "dir_perms_failed" 4

# --- Backup existing binary so we can restore on failure --------------------
had_existing=0
if [[ -f "$EXTENSION_PATH" ]]; then
    cp "$EXTENSION_PATH" "$BACKUP_PATH" && had_existing=1 && write_state "Backup" "$BACKUP_PATH"
fi

# --- Download to a temp path, validate it is an ELF for this arch -----------
tmp="$(mktemp)"
cleanup_tmp() { rm -f "$tmp"; }
trap cleanup_tmp EXIT

if ! curl -L --fail --silent --show-error -o "$tmp" "$URL"; then
    rm -f "$BACKUP_PATH" 2>/dev/null || true
    fail "download failed; confirm $URL is reachable from the host" "download_failed" 6
fi

# Validate: non-trivial size, ELF magic (7f 45 4c 46), and matching e_machine
# (offset 18, 2 bytes LE). Cheap, catches HTML 404 pages, truncated downloads,
# and an arch mismatch. Mirrors the windows_yellowkey MZ-header check.
if [[ ! -s "$tmp" ]] || [[ "$(stat -c%s "$tmp")" -lt 1024 ]]; then
    rm -f "$BACKUP_PATH" 2>/dev/null || true
    fail "downloaded asset is empty or too small" "invalid_payload" 6
fi
magic="$(od -An -tx1 -N4 "$tmp" | tr -d ' \n')"
machine="$(od -An -tx1 -j18 -N2 "$tmp" | tr -d ' \n')"
if [[ "$magic" != "7f454c46" ]]; then
    rm -f "$BACKUP_PATH" 2>/dev/null || true
    fail "downloaded asset is not an ELF binary (magic=$magic)" "invalid_payload" 6
fi
if [[ "$machine" != "$ELF_MACHINE" ]]; then
    rm -f "$BACKUP_PATH" 2>/dev/null || true
    fail "downloaded asset arch ($machine) does not match host ($ELF_MACHINE)" "arch_mismatch" 6
fi
write_state "Validated" "ELF ok ($arch)"

# --- Move into place; replace via rename so a loaded inode is untouched -----
if ! mv -f "$tmp" "$EXTENSION_PATH"; then
    [[ $had_existing -eq 1 && -f "$BACKUP_PATH" ]] && mv -f "$BACKUP_PATH" "$EXTENSION_PATH"
    fail "could not move binary to $EXTENSION_PATH" "move_failed" 4
fi
trap - EXIT  # tmp consumed by mv

# osquery rejects a non-root-owned or world-writable extension.
chown root:root "$EXTENSION_PATH" && chmod 0700 "$EXTENSION_PATH" \
    || fail "could not set perms on $EXTENSION_PATH" "binary_perms_failed" 4
write_state "Placed" "$EXTENSION_PATH (root:root 0700)"

# --- Autoload file: exactly our path, root:root 0640 ------------------------
if ! { echo "$EXTENSION_PATH" > "$AUTOLOAD_FILE"; }; then
    fail "could not write $AUTOLOAD_FILE" "loader_write_failed" 4
fi
chown root:root "$AUTOLOAD_FILE" && chmod 0640 "$AUTOLOAD_FILE" \
    || fail "could not set perms on $AUTOLOAD_FILE" "loader_perms_failed" 4
write_state "extensions.load" "written"

# --- Point orbit at /var/lib/fleetd via a systemd drop-in (image-mode safe) -
# The drop-in is the canonical override and survives fleetd/image upgrades; an
# edit to the package-managed /etc/default/orbit gets overwritten on upgrade.
if ! mkdir -p "$ORBIT_DROPIN_DIR"; then
    fail "could not create $ORBIT_DROPIN_DIR" "dropin_dir_failed" 4
fi
{
    cat > "$ORBIT_DROPIN" <<EOF
# Managed by install-rpm-ostree-extension.sh — relocates orbit's root to a
# writable path on image-mode hosts and autoloads the rpm_ostree extension.
[Service]
Environment=ORBIT_ROOT_DIR=$ORBIT_ROOT_DIR
Environment=ORBIT_OSQUERY_EXTENSIONS_AUTOLOAD=$AUTOLOAD_FILE
EOF
    chmod 0644 "$ORBIT_DROPIN"
} || fail "could not write $ORBIT_DROPIN" "dropin_write_failed" 4
write_state "orbit drop-in" "$ORBIT_DROPIN"

# Mirror the same vars into the package EnvironmentFile for compatibility with
# the documented fix (harmless if a later upgrade reverts it; the drop-in wins).
upsert_default() {
    local key="$1" value="$2" file="$3"
    touch "$file"
    if grep -qE "^[[:space:]]*${key}=" "$file"; then
        sed -i "s|^[[:space:]]*${key}=.*|${key}=${value}|" "$file"
    else
        echo "${key}=${value}" >> "$file"
    fi
}
{
    upsert_default "ORBIT_ROOT_DIR" "$ORBIT_ROOT_DIR" "$ORBIT_DEFAULTS"
    upsert_default "ORBIT_OSQUERY_EXTENSIONS_AUTOLOAD" "$AUTOLOAD_FILE" "$ORBIT_DEFAULTS"
    chmod 0644 "$ORBIT_DEFAULTS"
} || fail "could not update $ORBIT_DEFAULTS" "orbit_defaults_failed" 4
write_state "orbit defaults" "configured"

# --- Reload units (drop-in) and restart orbit, confirm it comes back active -
if ! systemctl daemon-reload; then
    fail "systemctl daemon-reload failed" "daemon_reload_failed" 4
fi
if ! systemctl restart "$SERVICE_NAME"; then
    [[ $had_existing -eq 1 && -f "$BACKUP_PATH" ]] && mv -f "$BACKUP_PATH" "$EXTENSION_PATH"
    fail "could not restart $SERVICE_NAME" "restart_failed" 5
fi
sleep 5
if ! systemctl is-active --quiet "$SERVICE_NAME"; then
    fail "$SERVICE_NAME did not return to active after restart" "service_not_active" 5
fi
write_state "Service" "active"

# Clean up the backup now that the install succeeded.
rm -f "$BACKUP_PATH" 2>/dev/null || true

echo ""
echo "OK: installed $EXTENSION_NAME at $EXTENSION_PATH."
echo "    orbit root + autoload set via $ORBIT_DROPIN (survives upgrades)."
echo "    orbit autoloads the binary via $AUTOLOAD_FILE on restart."
echo "    Confirm it loaded:"
echo "      journalctl -b -u $SERVICE_NAME | grep -i $EXTENSION_NAME"
echo "    Verify in Fleet (live query):"
echo "      SELECT 1 FROM osquery_registry"
echo "        WHERE registry = 'table' AND name = '${EXTENSION_NAME}_deployments' AND active = 1;"
echo "      SELECT id, version, booted, container_image_reference FROM ${EXTENSION_NAME}_deployments;"
write_state "State" "installed"
exit 0
