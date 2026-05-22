# Secure Boot Cert Update Osquery Extension (Go)

A Windows-only osquery extension that surfaces a device's progress through Microsoft's [Secure Boot 2023 certificate rollout](https://techcommunity.microsoft.com/blog/windows-itpro-blog/secure-boot-playbook-for-certificates-expiring-in-2026/4469235). The 2011-era Secure Boot certificates pre-loaded into every UEFI Windows PC since ~2012 expire starting June 2026.

## Table: `secureboot_cert_update`

### Derived state

| Column                   | Type    | Description |
|--------------------------|---------|-------------|
| `state`                  | TEXT    | See the table below for logic on how this value is calculated |
| `state_reason`           | TEXT    | Short human-readable explanation |
| `needs_action`           | INTEGER | 1 if an admin should look at this device |
| `action`                 | TEXT    | Recommended next step: `none`, `wait`, `reboot`, `enable_secure_boot`, `apply_manual_override`, `oem_firmware_required`, `investigate` |
| `days_until_cert_expiry` | INTEGER | Days until the first 2011 cert expires (KEK CA, June 24 2026) |

## How `state` is derived

A short-circuit ladder — first match wins, evaluated top to bottom.

| # | State | Trigger |
|---|-------|---------|
| 1 | `SecureBootDisabled` | `secureboot_enabled = 0` |
| 2 | `Updated` | `uefica2023_status = "Updated"` |
| 3 | `BlockedOEMMissingKEK` | Event 1803 seen → `missing_kek = 1` |
| 4 | `BlockedKnownIssue` | Event 1802 seen → `known_issue_id` populated |
| 5 | `BlockedFirmwareError` | `uefica2023_error` non-empty |
| 6 | `RebootPending` | Event 1800/1801 seen → `reboot_pending = 1` |
| 7 | `InProgress` | `available_updates != 0` and `uefica2023_status != "NotStarted"` |
| 8 | `ServicingBroken` | Scheduled task disabled, or `wincs_key_applied = 0` |
| 9 | `OptedOut` | `high_confidence_optout = 1` |
| 10 | `WaitingOnRollout` | `uefica2023_status = "NotStarted"` or `confidence_level` contains "observation" |
| 11 | `Unknown` | Nothing above matched |

`needs_action = 1` for `SecureBootDisabled`, `BlockedOEMMissingKEK`, `BlockedKnownIssue`, `BlockedFirmwareError`, and `ServicingBroken`. All other states are monitor-only.

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

### Raw data

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
| `bucket_id` | `HKLM\...\SecureBoot\Servicing` → `BucketId` |
| `confidence` | `HKLM\...\SecureBoot\Servicing` → `Confidence` |
| `skip_reason_known_issue` | `HKLM\...\SecureBoot\Servicing` → `SkipReasonKnownIssue` |
| `can_attempt_update_after` | `HKLM\...\SecureBoot\Servicing` → `CanAttemptUpdateAfter` |
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

## Usage with Fleet

```powershell
'C:\Program Files\Orbit\bin\orbit\orbit.exe' shell -- --extension .\secureboot_cert_update-amd64.exe --allow-unsafe
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
