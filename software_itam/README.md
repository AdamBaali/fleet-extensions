# software_itam osquery extension

One row per installed app or package with the fields an IT asset management (ITAM) export needs and Fleet's software inventory doesn't return yet: a publisher name, an install date, and the first time this host was seen with the item.

It reads the same native sources osquery's own tables read, then picks the best install date per item so an exporter needs one query instead of a join across several reports. It adds no last used date, because Fleet's software inventory already returns `last_opened_at` on macOS, Windows and Linux.

## Table schema

| Column | Type | Description |
|---|---|---|
| `source` | TEXT | `apps` (macOS), `programs` (Windows), `rpm_packages` or `deb_packages` (Linux). Same values as Fleet's software `source`, so rows join to Fleet's inventory |
| `name` | TEXT | Same name rule as Fleet's inventory (macOS: display name, bundle name, executable, folder name) |
| `version` | TEXT | App short version, `DisplayVersion`, or package version (RPM release is separate, as in Fleet) |
| `identifier` | TEXT | Bundle identifier, Uninstall registry key name, or package name |
| `path` | TEXT | App bundle path (macOS) or `InstallLocation` (Windows). Empty for packages |
| `arch` | TEXT | Package architecture (Linux) |
| `publisher` | TEXT | Publisher name. See publisher sources below |
| `publisher_source` | TEXT | Where the publisher came from |
| `install_date` | BIGINT | Best install date in unix seconds. `0` when unknown |
| `install_date_source` | TEXT | Which candidate gave `install_date` |
| `install_date_candidates` | TEXT | Every candidate found, as `source=unix` pairs separated by commas |
| `first_seen` | BIGINT | First time this extension saw the item on this host, in unix seconds |

### Install date sources, in order of preference
| OS | Sources |
|---|---|
| macOS | `install_history` (earliest matching entry in `/Library/Receipts/InstallHistory.plist`, kept across updates), `spotlight_date_added` (`kMDItemDateAdded`), `bundle_created` (bundle birth time) |
| Windows | `registry_install_date` (`InstallDate` value), `registry_key_written` (Uninstall key last write time) |
| Linux RPM | `rpm_install_time` |
| Linux DEB | `dpkg_log` (earliest `install` line in `/var/log/dpkg.log*`, including rotated `.gz` logs), `dpkg_list_changed` (package file list modified time, changes on upgrade) |

### Publisher sources
| OS | Sources |
|---|---|
| macOS | `codesign_authority` (signing certificate, for example `Developer ID Application: Example Inc (TEAMID1234)` becomes `Example Inc`), `apple` (Apple-signed apps), `copyright` (`NSHumanReadableCopyright`, used for App Store apps, which Apple re-signs) |
| Windows | `registry_publisher` (`Publisher` value) |
| Linux | `rpm_vendor`, `deb_maintainer` |

## State file

The extension keeps `software_itam_state.json` next to its binary (mode 0600). It holds each item's first and last seen dates and a cache of macOS code signature results, keyed by bundle path and `Info.plist` modified time, so an hourly query runs `codesign` only for new or updated apps. Items not seen for 400 days are dropped. A missing or corrupt file starts over and the query still returns rows.

## Building

```bash
make deps
make test
make build       # all six binaries, CGO off, from any OS
make checksums   # sha256 of each binary, for installers that pin them
```

## Usage

### With Fleet
```bash
sudo orbit shell -- --extension software_itam-arm64.ext --allow-unsafe
```

### With standard osquery
```bash
osqueryi --extension=/path/to/software_itam-amd64.ext --allow_unsafe
```

### Example queries

```sql
-- Everything, for an export
SELECT * FROM software_itam;

-- Apps with no install date from any source
SELECT name, path FROM software_itam WHERE install_date = 0;

-- How often each install date source was used
SELECT source, install_date_source, count(*) FROM software_itam GROUP BY 1, 2;
```

## Limits

- `install_history` matches on the app's display name, so an app renamed after install falls back to Spotlight.
- `registry_key_written` moves when a repair or update rewrites the Uninstall key.
- `dpkg_log` covers what logrotate kept, usually about a year. Older packages fall back to `dpkg_list_changed`.
- Windows and Linux use the registry and package databases, so the binaries run without CGO. macOS calls `/usr/bin/mdls` once per 200 apps and `/usr/bin/codesign` once per new or changed app.

## License

Same as the parent project.
