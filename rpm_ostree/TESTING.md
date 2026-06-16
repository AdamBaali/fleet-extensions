# Testing — rpm_ostree_deployments Extension

## Summary

| Layer | Method | Status |
|-------|--------|--------|
| Parsing / formatting logic | Go unit tests (`go test`) | ✅ Pass (8 tests, 14 subtests) |
| Parser vs real host JSON | opt-in test against live `rpm-ostree status --json` capture | ✅ Pass |
| Cross-compilation | `make all` (amd64 + arm64, static ELF) | ✅ Both build |
| `go vet` / `gofmt` | static analysis | ✅ Clean |
| Orbit/osquery integration | deploy via SSH + `journalctl` | ✅ Registered cleanly, no panic/respawn |
| Fleet live query | end-to-end via Fleet API (live query websocket) | ✅ 3 rows, all values match host ground truth |

## Unit Tests

The parsing logic is decoupled from the `rpm-ostree` subprocess call (`parseDeployments([]byte)`)
so it can be tested on any platform without an rpm-ostree host. Run:

```bash
make test        # or: go test -v ./...
```

Tests use a realistic `rpm-ostree status --json` fixture reconstructed from the real
Bluefin host (same deployment IDs, checksums, version, and container image as the
production query results below).

Coverage: **52.5%** of statements — this is the full parsing/formatting surface;
the uncovered remainder is the osquery server bootstrap in `main()` and the
`exec` call to `rpm-ostree`, which require a live Linux host.

### What the tests cover

- **3-deployment parse** (booted / staged / rollback) — correct `booted`/`staged` flags, version, container image, `base_checksum` present on layered deployments and empty on the plain base image.
- **`layered_packages` merge** — repo packages (`requested-packages`) + local RPMs (`requested-local-packages`), de-duplicated and sorted.
- **`manifest_digest` extraction** from the nested `base-commit-meta["ostree.manifest-digest"]` (handles nil meta, missing key, non-string value).
- **`timestamp` regression** — guards the bug fix described below.
- **Edge cases** — malformed JSON returns an error (logged, no rows), empty/missing `deployments`, and forward-compatibility with unknown top-level JSON keys.

## Bug Found & Fixed

**`formatTimestamp` produced garbage instead of the epoch.** The original code was:

```go
return string(rune(ts))   // BUG
```

`string(rune(ts))` interprets the integer as a single Unicode code point, not the
number. For a real timestamp it emits the U+FFFD replacement character. The test
log captures it directly:

```
fixed="1718323200"  old-buggy="�" (len=3 bytes)
```

Fixed to `strconv.FormatInt(ts, 10)`, so the integer `timestamp` column now holds
the real Unix epoch and is queryable (e.g. `WHERE timestamp > strftime('%s','now')-86400`).
This shipped undetected because the earlier production query did not select `timestamp`.
The post-fix live query (below) now returns real 10-digit epochs.

## Bug Found & Fixed (2): cold-start panic / "respawning too quickly"

On a cold start (orbit restart) the extension can launch before osqueryd has created
its Unix socket. The osquery-go SDK default socket-wait timeout is only **1 second**,
so `NewExtensionManagerServer` returned an error and the extension `panic`ed; osquery's
watcher then logged `Extension respawning too quickly` and relaunched it. It self-healed
but logged a panic stack trace on every restart. Observed in `journalctl`:

```
panic: waiting for unix socket to be available: /var/lib/fleetd/orbit-osquery.em: context deadline exceeded
W ... watcher.cpp:739] Extension respawning too quickly: /var/lib/fleetd/extensions/rpm_ostree.ext
```

Fixed by passing `osquery.ServerTimeout(10 * time.Second)` so the extension waits for the
socket and connects on the first launch, and by replacing `panic` with `log.Fatalf` for a
clean, logged exit. After redeploy, a fresh orbit restart showed a single clean
`Registering extension (rpm_ostree, …)` line and **zero** panic/respawn lines.

## Improvement: `layered_packages`

Previously the column only read `requested-local-packages`, so repo-layered packages
(e.g. a `vim` layered with `rpm-ostree install vim`) were invisible. It now merges
`requested-packages` + `requested-local-packages`, de-duplicated and sorted. The local
RPM case (`fleet-osquery-...`) still works exactly as before.

## Fleet Live Query — Verified end-to-end (post-fix, against the live host)

After deploying the fixed binary to the Bluefin LTS host (`id=3`), the query was run
live through the **Fleet API** (`POST /api/latest/fleet/queries/run` + the live-query
results websocket — the same path the Fleet UI uses):

```sql
SELECT id, version, booted, staged, base_checksum, container_image_reference,
       container_image_reference_digest, manifest_digest, layered_packages,
       osname, pinned, unlocked, timestamp
FROM rpm_ostree_deployments;
```

Result: **3 rows**, one host, `status: finished`, no errors. Every value was diffed
against the host's own `rpm-ostree status --json` and matched exactly:

| booted | staged | id (truncated) | base_checksum | layered_packages | timestamp |
|--------|--------|----------------|---------------|------------------|-----------|
| 1 | 0 | default-259867…a154.0 | 42cbb761… | fleet-osquery-1.54.0-1.aarch64 | `1778591568` |
| 0 | 1 | default-711cc6…b13e77.0 | 090f5e68… | fleet-osquery-1.54.0-1.aarch64 | `1781484615` |
| 0 | 0 | default-42cbb7…9a3ea55.0 | (empty — plain base) | (none) | `1757987193` |

The `timestamp` column now returns real 10-digit Unix epochs (it would have been the
`�` replacement character before the fix). All three deployments share the same
container image (`ostree-image-signed:docker://ghcr.io/ublue-os/bluefin:lts`): the
booted deployment layers `fleet-osquery` on base `42cbb…`; the staged deployment is a
newer base (`090f5e…`) with `fleet-osquery` re-layered; the third is the original base.

## Deployment Checklist (Bluefin host)

- [x] Binary compiled for arm64 and amd64 (static ELF)
- [x] Installed to `/var/lib/fleetd/extensions/rpm_ostree.ext` (root:root, 0700)
- [x] `--extension=/var/lib/fleetd/extensions/rpm_ostree.ext` added to `/var/lib/fleetd/osquery.flags`
- [x] orbit restarted; extension registered (`Registering extension (rpm_ostree, …)` in journalctl)
- [x] Fleet live query returns correct data

## Debugging

```bash
# Extension process running?
ps aux | grep rpm_ostree.ext

# Registered with osqueryd?
journalctl -b -u orbit.service | grep "Registering extension.*rpm_ostree"

# Extension errors (binary missing / JSON parse) now log to stderr:
journalctl -b -u orbit.service | grep "rpm_ostree_deployments:"

# Extension flag present?
grep extension /var/lib/fleetd/osquery.flags
```
