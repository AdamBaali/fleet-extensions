# Secure Boot Cert Update Osquery Extension (Go)

A Windows-only osquery extension that surfaces a device's progress through Microsoft's [Secure Boot 2023 certificate rollout](https://techcommunity.microsoft.com/blog/windows-itpro-blog/secure-boot-playbook-for-certificates-expiring-in-2026/4469235). The 2011-era Secure Boot certificates pre-loaded into every UEFI Windows PC since ~2012 expire starting June 2026; this table tells you, fleet-wide, which devices have already rolled to the 2023 certs and which need attention.

It's a read-only table — it does not enroll certificates or modify any registry values.

## Table: `secureboot_cert_update`

One row per device. Columns are grouped into derived state, raw playbook signals, hardware identity, and cert lifecycle.

### Derived state

The whole point of the extension. Compute once on the device; query simply in SQL.

| Column                   | Type    | Description |
|--------------------------|---------|-------------|
| `state`                  | TEXT    | One of the values in the table below |
| `state_reason`           | TEXT    | Short human-readable explanation |
| `needs_action`           | INTEGER | 1 if an admin should look at this device |
| `action`                 | TEXT    | Recommended next step: `none`, `wait`, `reboot`, `enable_secure_boot`, `apply_manual_override`, `oem_firmware_required`, `investigate` |
| `days_until_cert_expiry` | INTEGER | Days until the first 2011 cert expires (KEK CA, June 24 2026) |

#### `state` values

| State | Meaning | `needs_action` |
|-------|---------|---------------:|
| `Updated` | `UEFICA2023Status = Updated`. Terminal success. | 0 |
| `InProgress` | Rollout actively running (`AvailableUpdates != 0`, no errors). | 0 |
| `RebootPending` | Update staged; awaiting reboot to complete. | 0 |
| `WaitingOnRollout` | Secure Boot enabled, no errors, awaiting Microsoft's phased rollout to promote this firmware bucket. | 0 |
| `OptedOut` | `HighConfidenceOptOut = 1`. Intentional. | 0 |
| `SecureBootDisabled` | UEFI device with Secure Boot turned off. Rollout cannot run. | 1 |
| `BlockedOEMMissingKEK` | Event 1803 — firmware has no PK-signed KEK update slot. OEM firmware update required. | 1 |
| `BlockedKnownIssue` | Event 1802 — Microsoft has identified a `KI_*` issue blocking this device. | 1 |
| `BlockedFirmwareError` | `UEFICA2023Error` populated. | 1 |
| `ServicingBroken` | Scheduled task disabled or `WinCSKeyApplied = 0`. Rollout infrastructure itself is wrong. | 1 |
| `Unknown` | Could not read enough state to classify (e.g. servicing key not present yet). | 0 |

### Raw playbook signals

These mirror the registry/event-log fields Microsoft's [Detect-SecureBootCertUpdateStatus.ps1](https://support.microsoft.com/en-us/topic/registry-key-updates-for-secure-boot-windows-devices-with-it-managed-updates-fdc97f0d-eb30-432a-8c43-be8de2a93d6c) reads. Use them for forensics when the derived `state` isn't enough.

| Column | Source |
|--------|--------|
| `secureboot_enabled` | `HKLM\...\SecureBoot\State` → `UEFISecureBootEnabled` |
| `uefica2023_status` | `HKLM\...\SecureBoot\Servicing` → `UEFICA2023Status` |
| `uefica2023_error` | `HKLM\...\SecureBoot\Servicing` → `UEFICA2023Error` |
| `available_updates` | `HKLM\...\SecureBoot` → `AvailableUpdates` (hex) |
| `available_updates_policy` | `HKLM\...\SecureBoot` → `AvailableUpdatesPolicy` (hex) |
| `high_confidence_optout` | `HKLM\...\SecureBoot` → `HighConfidenceOptOut` |
| `microsoft_update_managed_optin` | `HKLM\...\SecureBoot` → `MicrosoftUpdateManagedOptIn` |
| `bucket_hash` | `HKLM\...\SecureBoot\Servicing` → `BucketHash` |
| `confidence_level` | `HKLM\...\SecureBoot\Servicing` → `ConfidenceLevel` |
| `windows_uefica2023_capable` | `HKLM\...\SecureBoot\Servicing` → `WindowsUEFICA2023Capable` |
| `skip_reason_known_issue` | `HKLM\...\SecureBoot\Servicing` → `SkipReasonKnownIssue` |
| `can_attempt_update_after` | `HKLM\...\SecureBoot\Servicing\DeviceAttributes` → `CanAttemptUpdateAfter` (REG_BINARY; decoded as FILETIME when 8 bytes, else hex) |
| `secureboot_task_status` | `schtasks /Query` against `\Microsoft\Windows\PI\Secure-Boot-Update` |
| `secureboot_task_enabled` | Same task — `Scheduled Task State = Enabled` |
| `wincs_key_status` | `HKLM\...\SecureBoot\Servicing` → `WinCSKeyStatus` |
| `wincs_key_applied` | `HKLM\...\SecureBoot\Servicing` → `WinCSKeyApplied` |
| `reboot_pending` | Derived from Event IDs 1800/1801 |
| `missing_kek` | Derived from Event ID 1803 |
| `known_issue_id` | Event ID 1802 message, regex `KI_\d+` |
| `latest_event_id` | Most recent `Microsoft-Windows-Kernel-Boot` event in 1795–1808 |
| `latest_event_time` | Timestamp of `latest_event_id` |
| `last_boot_time` | Now minus `GetTickCount64()` |
| `collection_time` | When this row was produced |

### Hardware identity

Useful for fleet-wide `GROUP BY` to spot OEM/firmware combinations stuck on the same bucket. The Microsoft-curated values under `\Servicing\DeviceAttributes` are preferred; the `\HARDWARE\DESCRIPTION\System\BIOS` subkey is the fallback when the rollout hasn't populated `DeviceAttributes` yet.

| Column | Primary source | Fallback |
|--------|----------------|----------|
| `oem_name` | `…\Servicing\DeviceAttributes` → `OEMName` | — |
| `oem_manufacturer_name` | `…\DeviceAttributes` → `OEMManufacturerName` | `…\BIOS` → `SystemManufacturer` |
| `oem_model_system_family` | `…\DeviceAttributes` → `OEMModelSystemFamily` | `…\BIOS` → `SystemFamily` |
| `oem_model_number` | `…\DeviceAttributes` → `OEMModelNumber` | `…\BIOS` → `SystemProductName` |
| `firmware_manufacturer` | `…\DeviceAttributes` → `FirmwareManufacturer` | — |
| `firmware_version` | `…\DeviceAttributes` → `FirmwareVersion` | `…\BIOS` → `BIOSVersion` |
| `firmware_release_date` | `…\DeviceAttributes` → `FirmwareReleaseDate` | `…\BIOS` → `BIOSReleaseDate` |
| `baseboard_manufacturer` | `…\DeviceAttributes` → `BaseBoardManufacturer` | `…\BIOS` → `BaseBoardManufacturer` |
| `baseboard_product` | `…\DeviceAttributes` → `OEMModelBaseBoard` | `…\BIOS` → `BaseBoardProduct` |
| `os_version` | `HKLM\SOFTWARE\Microsoft\Windows NT\CurrentVersion` | — |
| `os_architecture` | `…\DeviceAttributes` → `OSArchitecture` | `PROCESSOR_ARCHITECTURE` env var |

### Cert lifecycle

| Column | Value |
|--------|-------|
| `cert_kek_2011_expires_at` | `2026-06-24T00:00:00Z` |
| `cert_uefi_ca_2011_expires_at` | `2026-06-27T00:00:00Z` |
| `cert_windows_pca_2011_expires_at` | `2026-10-19T00:00:00Z` |
| `extension_schema_version` | Schema version of this extension |

## Building the Extension

Cross-compiles from any host:

```bash
cd secureboot_cert_update
make deps
make windows
```

Produces:

- `secureboot_cert_update-amd64.exe` — 64-bit Intel/AMD Windows
- `secureboot_cert_update-arm64.exe` — 64-bit ARM Windows

## Usage

### With Fleet

```powershell
orbit shell.exe -- --extension secureboot_cert_update-amd64.exe --allow-unsafe
```

For daily collection across a fleet, deploy the `.exe` to `C:\Program Files\Orbit\osquery_extensions\` (or your equivalent) and configure Fleet's extensions autoload.

### With standalone osquery

```powershell
osqueryi.exe --extension=C:\path\to\secureboot_cert_update-amd64.exe
```

## Example queries

```sql
-- One-row fleet health summary, grouped by state
SELECT state, COUNT(*) AS hosts
FROM secureboot_cert_update
GROUP BY state;
```

```sql
-- What needs my attention right now
SELECT
  state, state_reason, action,
  oem_manufacturer_name, oem_model_number, firmware_version
FROM secureboot_cert_update
WHERE needs_action = 1;
```

```sql
-- Which OEMs are the long pole (devices stuck on missing-KEK firmware)
SELECT
  oem_manufacturer_name, oem_model_number, firmware_version,
  COUNT(*) AS affected
FROM secureboot_cert_update
WHERE state = 'BlockedOEMMissingKEK'
GROUP BY 1, 2, 3
ORDER BY affected DESC;
```

```sql
-- Buckets stuck in observation that could be unblocked by manual override
SELECT
  bucket_hash, confidence_level, oem_model_number, firmware_version,
  COUNT(*) AS device_count
FROM secureboot_cert_update
WHERE state = 'WaitingOnRollout'
GROUP BY bucket_hash, oem_model_number, firmware_version
HAVING device_count > 5
ORDER BY device_count DESC;
```

```sql
-- Stragglers within 60 days of first cert expiry
SELECT hostname, state, state_reason
FROM secureboot_cert_update
WHERE state NOT IN ('Updated', 'Unknown')
  AND days_until_cert_expiry < 60;
```

## Permissions

Requires the rights normally granted to osqueryd:

- **Registry**: `HKLM\SYSTEM\CurrentControlSet\Control\SecureBoot` and `\Servicing` are readable by `Authenticated Users`; no elevation needed.
- **Scheduled task query**: `schtasks /Query` against `\Microsoft\Windows\PI\Secure-Boot-Update` works for any local user.
- **Event log**: `Get-WinEvent` against `System` requires membership in `Event Log Readers` (or SYSTEM/Administrator). When osqueryd runs as `LocalSystem` (the default Fleet/orbit configuration) this is fine; if you ever test the extension interactively as a normal user, event-derived columns (`missing_kek`, `known_issue_id`, `latest_event_*`) will be empty and the state may degrade to `Unknown`.

## Structure

```
secureboot_cert_update/
├── main.go      # Extension code + state machine
├── go.mod       # Module definition
├── Makefile     # Cross-build for amd64/arm64 Windows
└── README.md    # This file
```

## Requirements

- Go 1.21 or later (build host)
- Windows 10 / Windows 11 / Windows Server 2016+ (target host)
- osquery 5.0+ or Fleet

## License

Same as the parent project.
