# rpm-ostree Extension

Exposes rpm-ostree deployment state as a native osquery table, `rpm_ostree_deployments`, for atomic / image-mode hosts (Fedora Silverblue/Kinoite, Universal Blue / Bluefin, Fedora CoreOS, and similar).

## Description

Stock osquery (including the build orbit bundles) has no table for rpm-ostree state. On image-mode systems there is no native way to see which deployment is booted, what container image it came from, or which packages have been layered on top. This extension fills that gap by running `rpm-ostree status --json` and emitting one row per deployment.

## Platforms

- **Linux** (rpm-ostree based atomic hosts; requires the `rpm-ostree` binary)
- **Binaries:** `rpm_ostree-amd64.ext`, `rpm_ostree-arm64.ext`
- **Installation:** Automated install script for atomic hosts running Fleet orbit

## Table Schema

### rpm_ostree_deployments

One row per entry in `rpm-ostree status --json` → `.deployments[]`.

| Column | Type | Description |
|--------|------|-------------|
| id | TEXT | Deployment id (e.g. `default-<checksum>.0`) |
| version | TEXT | Deployment version string |
| checksum | TEXT | OSTree commit checksum for the deployment |
| booted | INTEGER | `1` if this is the currently booted deployment, else `0` |
| staged | INTEGER | `1` if this deployment is staged for next boot, else `0` |
| container_image_reference | TEXT | Source container image ref, raw (e.g. `ostree-image-signed:docker://ghcr.io/ublue-os/bluefin:lts`) |
| layered_packages | TEXT | JSON array of packages layered on the base image (`[]` when none) |
| error | TEXT | Error message if collection failed (e.g. host is not rpm-ostree based) |

On a host where `rpm-ostree` is unavailable, or its output cannot be parsed, the table returns a single row with `error` populated and the other columns blank.

## Example Queries

### All deployments

```sql
SELECT id, version, booted, staged, container_image_reference, layered_packages
FROM rpm_ostree_deployments;
```

### The currently booted deployment

```sql
SELECT version, checksum, container_image_reference
FROM rpm_ostree_deployments
WHERE booted = 1;
```

### Hosts with layered packages (drift off the base image)

```sql
SELECT id, layered_packages
FROM rpm_ostree_deployments
WHERE booted = 1 AND layered_packages != '[]';
```

### Fleet-wide image inventory

```sql
SELECT container_image_reference, COUNT(*) AS hosts
FROM rpm_ostree_deployments
WHERE booted = 1
GROUP BY container_image_reference;
```

## Installation

### Automated installation (atomic hosts running orbit)

Use the included `install-rpm-ostree-extension.sh` via Fleet's script execution (or run it directly with sudo):

```bash
sudo ./install-rpm-ostree-extension.sh
```

The installer is built for Fleet's script execution: it is idempotent (rerunning re-asserts the binary, loader, perms, and orbit config), emits structured `key : value` output, and uses numbered exit codes so a failed run is legible in Fleet's script results. It:

1. Pre-flight checks: runs as root, host is rpm-ostree based, `orbit.service` exists
2. Detects the architecture (amd64/arm64) and downloads the matching binary from this repo's latest GitHub release
3. Validates the download is an ELF binary for the host architecture (magic + `e_machine`), backing up and restoring the previous binary on any failure
4. Installs it to `/var/lib/fleetd/extensions/rpm_ostree.ext`, owned `root:root`, mode `0700` (osquery refuses a non-root-owned or world-writable extension)
5. Writes the autoload file `/var/lib/fleetd/extensions.load` (mode `0640`)
6. Points orbit at `/var/lib/fleetd` by setting `ORBIT_ROOT_DIR` and `ORBIT_OSQUERY_EXTENSIONS_AUTOLOAD` via a systemd drop-in at `/etc/systemd/system/orbit.service.d/10-fleet-extensions.conf` (and mirrors them into `/etc/default/orbit`)
7. Runs `systemctl daemon-reload`, restarts `orbit.service`, and confirms it returns to active

| Exit code | Meaning |
|---|---|
| 0 | Installed; orbit back to active |
| 2 | Not run as root |
| 3 | `orbit.service` not present (is fleetd installed?) |
| 4 | Filesystem/config operation failed |
| 5 | orbit did not return to active after restart |
| 6 | Download failed or asset is not a valid ELF for this architecture |
| 7 | rpm-ostree not found (not an atomic host) |
| 8 | Unsupported architecture |

> **Why this layout (image-mode/atomic precedent)?**
> On image-mode systems (Bluefin/Silverblue/CoreOS) fleetd is rpm-ostree *layered*, so `/usr` and orbit's default root `/opt/orbit` are read-only, while `/etc` and `/var` are writable (`/opt` is itself a read-only symlink to `/var/opt`, and a `.mount` for `/opt/orbit` fails as "not canonical (contains a symlink)"). So the binary and autoload file live under `/var/lib/fleetd`, and orbit is relocated there with `ORBIT_ROOT_DIR` + `ORBIT_OSQUERY_EXTENSIONS_AUTOLOAD`.
>
> Those vars are delivered through a **systemd drop-in** (`/etc/systemd/system/orbit.service.d/10-fleet-extensions.conf`) rather than only editing the package-managed `/etc/default/orbit`. The drop-in is the canonical systemd override and survives fleetd/image upgrades, whereas a fleetd package upgrade overwrites `/etc/default/orbit` ([fleetdm/fleet#18365](https://github.com/fleetdm/fleet/issues/18365)). The installer writes both for belt-and-suspenders; the drop-in wins.
>
> To remove: delete `/etc/systemd/system/orbit.service.d/10-fleet-extensions.conf`, the binary, and the loader, then `systemctl daemon-reload && systemctl restart orbit`.

Confirm the extension loaded:

```bash
journalctl -b -u orbit.service | grep -i rpm_ostree
```

Or verify from Fleet with a live query that the table registered and is active:

```sql
SELECT 1 FROM osquery_registry
WHERE registry = 'table' AND name = 'rpm_ostree_deployments' AND active = 1;
```

## Testing on an atomic host (without the Fleet UI)

`rpm_ostree_deployments` can be smoke-tested against an ephemeral osqueryd using the osqueryd that orbit already ships. On an arm64 host:

```bash
OSQUERYD=/var/lib/fleetd/bin/osqueryd/linux-arm64/stable/osqueryd   # linux-x86_64 on amd64
EXT=/var/lib/fleetd/extensions/rpm_ostree.ext
SOCK=/tmp/osq.sock

# 1. start an ephemeral osqueryd
sudo "$OSQUERYD" --ephemeral --disable_database --disable_logging \
  --extensions_socket="$SOCK" --extensions_timeout=10 &

# 2. start the extension against that socket
sudo "$EXT" --socket "$SOCK" &

# 3. query it
sudo "$OSQUERYD" -S --connect --extensions_socket="$SOCK" \
  "SELECT id, version, booted, staged, container_image_reference, layered_packages FROM rpm_ostree_deployments;"
```

Expect at least one row, with the booted deployment showing `booted = 1` and a `container_image_reference` matching the host image (e.g. `ghcr.io/ublue-os/bluefin:lts`).

A quick interactive check is also available without deploying:

```bash
sudo orbit shell -- --extension /var/lib/fleetd/extensions/rpm_ostree.ext --allow-unsafe
osquery> SELECT * FROM rpm_ostree_deployments;
```

## Building from source

```bash
cd rpm_ostree
make deps     # go mod tidy
make build    # produces rpm_ostree-amd64.ext and rpm_ostree-arm64.ext
make test     # runs the parser unit tests
```

Builds are static and CGO-free (`CGO_ENABLED=0`), so the binary runs on any atomic host regardless of libc.

## Requirements

- **rpm-ostree** present on the target host (`/usr/bin/rpm-ostree`)
- **osquery** or **Fleet (orbit)**
- **Go 1.26+** (build host only)

## Troubleshooting

### Extension returns `rpm-ostree not found`

The host is not rpm-ostree based, or `rpm-ostree` is not on `PATH` and not at `/usr/bin/rpm-ostree`.

### Extension not loading under orbit

Check the journal and confirm the binary permissions (osquery rejects a non-root-owned or world-writable extension):

```bash
journalctl -b -u orbit.service | grep -i extension
ls -la /var/lib/fleetd/extensions/rpm_ostree.ext   # expect -rwx------ root root
cat /var/lib/fleetd/extensions.load
grep ORBIT_ /etc/default/orbit
```

### Empty results

Run the command manually:

```bash
rpm-ostree status --json | jq '.deployments[] | {id, version, booted, staged}'
```

## License

This extension is part of the fleet-extensions project. See the repository LICENSE for details.
