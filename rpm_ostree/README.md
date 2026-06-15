# rpm_ostree_deployments Extension

Provides a table for querying rpm-ostree deployment status on rpm-ostree-based systems (e.g., Fedora Silverblue, Bluefin, etc.).

## Table: rpm_ostree_deployments

Returns one row per deployment. Includes information about booted, staged, and rollback deployments.

### Columns

| Column | Type | Description |
|--------|------|-------------|
| `id` | text | Deployment ID (includes checksum) |
| `version` | text | Deployment version string (e.g., "stream10.1") |
| `booted` | integer | 1 if this is the currently booted deployment, 0 otherwise |
| `staged` | integer | 1 if this deployment is staged for next boot, 0 otherwise |
| `checksum` | text | Deployment commit checksum |
| `base_checksum` | text | Base image checksum |
| `container_image_reference` | text | Container image reference (e.g., "ostree-image-signed:docker://ghcr.io/ublue-os/bluefin:lts") |
| `container_image_reference_digest` | text | Container image digest (SHA256) |
| `manifest_digest` | text | OSTree manifest digest (SHA256) |
| `layered_packages` | text | Sorted, comma-separated list of all layered packages — both repo-layered (`requested-packages`) and local RPM (`requested-local-packages`) |
| `osname` | text | OS name (typically "default") |
| `pinned` | integer | 1 if this deployment is pinned, 0 otherwise |
| `unlocked` | text | Unlocked state (usually "none") |
| `timestamp` | integer | Deployment timestamp |

### Example Query

```sql
SELECT id, version, booted, staged, container_image_reference
FROM rpm_ostree_deployments
WHERE booted = 1;
```

### Implementation Notes

- Parses output from `rpm-ostree status --json`
- Field names with hyphens in JSON are converted to underscores in the table (e.g., `base-checksum` → `base_checksum`)
- Boolean JSON values are converted to integers (1 or 0)
- `layered_packages` merges repo-layered packages (`requested-packages`) and local RPM packages (`requested-local-packages`), de-duplicated and sorted, joined with commas
- `timestamp` is the deployment's Unix epoch (seconds) as an integer
- Manifest digest is extracted from the nested `base-commit-meta` object in the JSON
- `rpm-ostree` is invoked at its absolute path (`/usr/bin/rpm-ostree`), with a PATH fallback, since osqueryd runs extensions with a minimal environment
- On any error (binary missing, malformed JSON) the table logs to stderr and returns no rows rather than an error row, which keeps the result schema-valid
- The extension waits up to 10s for osquery's socket on startup (`osquery.ServerTimeout`) so a cold start (e.g. orbit restart) connects on the first try instead of panicking and being respawned

### Building & Testing

```bash
make test     # run unit tests (parsing/formatting logic)
make all      # cross-compile rpm_ostree-amd64.ext and rpm_ostree-arm64.ext (static)
make vet      # go vet
```
