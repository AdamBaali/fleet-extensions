# YellowKey osquery extension (CVE-2026-45585)

A Windows-only osquery extension that returns one row of per-host verdict for the YellowKey BitLocker bypass ([CVE-2026-45585](https://msrc.microsoft.com/update-guide/vulnerability/CVE-2026-45585)). YellowKey abuses `autofstx.exe` inside the Windows Recovery Environment to drop an attacker into `cmd.exe` with the BitLocker volume unlocked; brief physical access is enough. Affects Windows 11, Server 2022, Server 2025. Microsoft shipped a mitigation on May 19 2026, no full patch yet.

## Table: `windows_yellowkey`

| Column | Type | Description |
|---|---|---|
| `state` | TEXT | Verdict (see below) |
| `state_reason` | TEXT | Short human-readable explanation |
| `needs_action` | INTEGER | `1` when `state = exposed` |
| `winre_enabled` | TEXT | `Enabled`, `Disabled`, or `unknown` |
| `tpm_only` | INTEGER | `1` when a BitLocker volume uses TPM without a PIN (what the public PoC targets) |
| `mitigated` | INTEGER | `1` when the `BootExecMitigated` marker is set |

## How `state` is derived

A short-circuit ladder, first match wins:

| # | State | Trigger |
|---|---|---|
| 1 | `not_affected` | Windows 10 or unrecognised SKU |
| 2 | `mitigated` | `HKLM\SOFTWARE\Fleet\YellowKey\BootExecMitigated` set |
| 3 | `mitigated_winre_off` | WinRE disabled (`reagentc /info`) |
| 4 | `bitlocker_off` | No protected BitLocker volume |
| 5 | `exposed` | Affected OS, BitLocker on, no mitigation |

`needs_action = 1` only for `exposed`.

## Inputs

Four signals the extension reads on every query:

| Signal | Source |
|---|---|
| OS family + build | Registry `ProductName` plus `CurrentBuild` (Windows 11 detection; `ProductName` still reads "Windows 10" on Win11) |
| WinRE state | `reagentc /info` |
| BitLocker key protectors | `Get-BitLockerVolume` over PowerShell |
| Mitigation marker | `HKLM\SOFTWARE\Fleet\YellowKey\BootExecMitigated` |

No snapshot file, no freshness gate; every query is live.

## Building the Extension

Cross-compiles from any host:

```bash
cd windows_yellowkey
make deps
make build
```

Produces `windows_yellowkey-amd64.exe` and `windows_yellowkey-arm64.exe`.

## Install

Use the included `install-windows-yellowkey-extension.ps1` via Fleet's script execution (or run it directly on the host in an elevated PowerShell):

```powershell
powershell -ExecutionPolicy Bypass -File .\install-windows-yellowkey-extension.ps1
```

The installer downloads the architecture-matching binary from this repo on `main`, verifies its SHA-256, places it under `C:\Program Files\osquery\extensions\`, adds the path to `C:\Program Files\osquery\extensions.load`, hardens the ACLs, and restarts the `Fleet osquery` service. Idempotent: rerunning is a no-op when the binary already matches.

## Usage with Fleet

For a quick interactive test without deploying:

```powershell
'C:\Program Files\Orbit\bin\orbit\orbit.exe' shell -- --extension .\windows_yellowkey-amd64.exe --allow-unsafe
```

## Example queries

```sql
-- Everything
SELECT * FROM windows_yellowkey;
```

```sql
-- Exposed hosts
SELECT * FROM windows_yellowkey WHERE state = 'exposed';
```

```sql
-- Exposed TPM-only hosts (most at risk)
SELECT * FROM windows_yellowkey WHERE state = 'exposed' AND tpm_only = 1;
```

```sql
-- Fleet-wide summary
SELECT state, COUNT(*) AS hosts FROM windows_yellowkey GROUP BY state;
```

## Structure

```
windows_yellowkey/
├── main.go                                  # Extension code + verdict ladder
├── go.mod                                   # Module definition
├── Makefile                                 # Cross-build for amd64/arm64 Windows
├── install-windows-yellowkey-extension.ps1  # Automated installer
└── README.md                                # This file
```

## Requirements

- Go 1.21 or later (build host)
- Windows 11 / Windows Server 2022+ (target host; Windows 10 returns `not_affected`)
- osquery 5.0+ or Fleet

## Caveats

- The WinRE status words (`Enabled` / `Disabled`) are English-only. On a non-English Windows install the parse falls through to `unknown`, and `state` becomes `exposed` (the safe default).
- `mitigated` reflects the `BootExecMitigated` marker, not a live read of the WinRE image. It is not auto-cleared; when Microsoft ships a patch, clear the marker to retire it.
- The interactive test above uses `--allow-unsafe`, which bypasses osquery's permission check. A binary that loads in that shell can still fail to autoload under orbit if its ACL is not hardened.

## License

Same as the parent project.
