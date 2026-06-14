#!/bin/bash

# rpm-ostree extension installer script
# Downloads and installs the rpm_ostree_deployments extension on rpm-ostree
# based atomic hosts (Fedora Silverblue/Kinoite, Universal Blue / Bluefin,
# Fedora CoreOS, etc.) running Fleet's orbit.
#
# Why this differs from the Ubuntu installers in this repo:
#   On image-mode systems /opt is a read-only symlink to /var/opt, so the usual
#   /var/fleetd/extensions + /etc/osquery/extensions.load layout that orbit
#   discovers under /opt is not writable. Instead we keep everything under
#   /var/lib/fleetd and point orbit at it with ORBIT_ROOT_DIR plus
#   ORBIT_OSQUERY_EXTENSIONS_AUTOLOAD in /etc/default/orbit.
#
# osquery refuses to load an extension binary that is not root-owned or is
# world-writable, so the binary is installed root:root mode 0700.
#
# Usage:
#   sudo ./install-rpm-ostree-extension.sh

set -e  # Exit on any error

# Variables
GITHUB_REPO="allenhouchins/fleet-extensions"
ORBIT_ROOT_DIR="/var/lib/fleetd"
EXTENSION_DIR="$ORBIT_ROOT_DIR/extensions"
AUTOLOAD_FILE="$ORBIT_ROOT_DIR/extensions.load"
ORBIT_DEFAULTS="/etc/default/orbit"
INSTALLED_NAME="rpm_ostree.ext"   # the .ext suffix is required by osquery
EXTENSION_PATH="$EXTENSION_DIR/$INSTALLED_NAME"
BACKUP_PATH=""

echo "Starting rpm-ostree Extension installation..."

log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1"
}

# Function to check if running as root
check_root() {
    if [[ $EUID -ne 0 ]]; then
        log "Error: This script must be run as root (use sudo)"
        exit 1
    fi
}

# Function to verify this is an rpm-ostree host
check_rpm_ostree() {
    log "Checking for rpm-ostree..."
    if ! command -v rpm-ostree &> /dev/null && [[ ! -x /usr/bin/rpm-ostree ]]; then
        log "Error: rpm-ostree not found. This extension targets atomic/image-mode hosts."
        exit 1
    fi
    log "rpm-ostree found"
}

# Function to detect architecture and set the release asset name
detect_architecture() {
    log "Detecting system architecture..."
    local arch
    arch=$(uname -m)
    case "$arch" in
        "x86_64")
            ASSET_NAME="rpm_ostree-amd64.ext"
            log "Architecture detected: amd64 (x86_64)"
            ;;
        "aarch64"|"arm64")
            ASSET_NAME="rpm_ostree-arm64.ext"
            log "Architecture detected: arm64 (aarch64)"
            ;;
        *)
            log "Error: Unsupported architecture: $arch"
            log "This script supports amd64 (x86_64) and arm64 (aarch64) only"
            exit 1
            ;;
    esac
    BACKUP_PATH="$EXTENSION_PATH.backup.$(date +%Y%m%d_%H%M%S)"
}

# Function to check prerequisites
check_prerequisites() {
    log "Checking prerequisites..."
    if ! command -v curl &> /dev/null; then
        log "Error: curl is required but not found. Install curl and re-run."
        exit 1
    fi
    log "curl is available"
}

# Function to create directory with proper ownership
create_directory() {
    local dir="$1"
    mkdir -p "$dir"
    chown root:root "$dir"
    chmod 0755 "$dir"
    log "Directory ready: $dir"
}

# Function to backup existing extension
backup_existing() {
    if [[ -f "$EXTENSION_PATH" ]]; then
        log "Backing up existing extension to: $BACKUP_PATH"
        cp "$EXTENSION_PATH" "$BACKUP_PATH"
    fi
}

# Function to download the latest release asset
download_latest_release() {
    log "Downloading $ASSET_NAME from the latest release..."
    local temp_file
    temp_file=$(mktemp)

    local direct_url="https://github.com/$GITHUB_REPO/releases/latest/download/$ASSET_NAME"
    log "Source: $direct_url"

    if ! curl -L --fail --progress-bar -o "$temp_file" "$direct_url"; then
        log "Error: Failed to download $ASSET_NAME"
        log "Verify it exists at https://github.com/$GITHUB_REPO/releases/latest"
        rm -f "$temp_file"
        exit 1
    fi

    if [[ ! -s "$temp_file" ]]; then
        log "Error: Downloaded file is empty"
        rm -f "$temp_file"
        exit 1
    fi

    mv "$temp_file" "$EXTENSION_PATH"
    log "Installed binary: $EXTENSION_PATH"
}

# Function to set the strict permissions osquery requires for an autoloaded ext
setup_file_permissions() {
    log "Setting file permissions (root:root, 0700)..."
    chown root:root "$EXTENSION_PATH"
    chmod 0700 "$EXTENSION_PATH"
}

# Function to write the autoload file (one path per line)
setup_autoload() {
    log "Writing autoload file: $AUTOLOAD_FILE"
    echo "$EXTENSION_PATH" > "$AUTOLOAD_FILE"
    chown root:root "$AUTOLOAD_FILE"
    chmod 0640 "$AUTOLOAD_FILE"
}

# upsert KEY=VALUE in a defaults file, replacing any existing definition
upsert_default() {
    local key="$1" value="$2" file="$3"
    touch "$file"
    if grep -qE "^[[:space:]]*${key}=" "$file"; then
        # Replace the existing line in place.
        sed -i "s|^[[:space:]]*${key}=.*|${key}=${value}|" "$file"
    else
        echo "${key}=${value}" >> "$file"
    fi
}

# Function to point orbit at /var/lib/fleetd and our autoload file
setup_orbit_defaults() {
    log "Configuring $ORBIT_DEFAULTS..."
    upsert_default "ORBIT_ROOT_DIR" "$ORBIT_ROOT_DIR" "$ORBIT_DEFAULTS"
    upsert_default "ORBIT_OSQUERY_EXTENSIONS_AUTOLOAD" "$AUTOLOAD_FILE" "$ORBIT_DEFAULTS"
    chmod 0644 "$ORBIT_DEFAULTS"
    log "orbit defaults configured"
}

# Function to restart orbit service
restart_orbit_service() {
    log "Restarting orbit service..."
    if systemctl is-active --quiet orbit.service; then
        if systemctl restart orbit.service; then
            log "orbit.service restarted successfully"
        else
            log "Warning: Failed to restart orbit.service"
        fi
    else
        log "Warning: orbit.service not active; extension loads on next start"
    fi
}

# Function to cleanup on failure
cleanup_on_failure() {
    log "Cleaning up due to failure..."
    if [[ -f "$EXTENSION_PATH" ]] && [[ -f "$BACKUP_PATH" ]]; then
        mv "$BACKUP_PATH" "$EXTENSION_PATH"
        log "Restored previous version from backup"
    fi
}

trap cleanup_on_failure ERR

main() {
    log "=== rpm-ostree Extension Installer Started ==="
    check_root
    check_rpm_ostree
    detect_architecture
    check_prerequisites
    create_directory "$EXTENSION_DIR"
    backup_existing
    download_latest_release
    setup_file_permissions
    setup_autoload
    setup_orbit_defaults
    restart_orbit_service

    if [[ -f "$BACKUP_PATH" ]]; then
        rm -f "$BACKUP_PATH"
    fi

    log "=== Installation completed successfully! ==="
    log "Extension:      $EXTENSION_PATH"
    log "Autoload file:  $AUTOLOAD_FILE"
    log "orbit defaults: $ORBIT_DEFAULTS"
    log "Verify with:    journalctl -b -u orbit.service | grep -i rpm_ostree"
    echo ""
}

main "$@"
