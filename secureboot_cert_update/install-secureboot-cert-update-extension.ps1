<#
.SYNOPSIS
    Installs and loads the secureboot_cert_update osquery extension on this host.

.DESCRIPTION
    Idempotent installer for the secureboot_cert_update osquery extension under
    fleetd on Windows. Mirrors the Linux/macOS installers in this repo:
    download the architecture-matching binary, place it where osquery's
    compiled-default autoload path will pick it up, and restart the service.

    The script writes to osquery's compiled-in default autoload path on
    Windows: `C:\Program Files\osquery\extensions.load`. orbit owns its own
    <root-dir>\extensions.load and only passes --extensions_autoload to osqueryd
    when that file is non-empty; ExtensionRunner keeps it empty unless Fleet
    has TUF-managed extensions configured. With nothing forcing a different
    autoload path, osqueryd falls back to its compiled default and reads our
    file. This is the Windows twin of the Linux/macOS pattern, which writes
    to /etc/osquery/extensions.load and /var/osquery/extensions.load.

    Flow:
      1. Pre-flight: Fleet osquery service present.
      2. Pick the URL and SHA-256 for this host's architecture.
      3. Ensure C:\Program Files\osquery\extensions exists, harden its ACL.
      4. Download the binary to a temp path, verify SHA-256, move into place.
      5. Harden the binary ACL.
      6. Add the binary path to C:\Program Files\osquery\extensions.load
         (idempotent), harden that file.
      7. Restart the Fleet osquery service so osqueryd autoloads the extension.

.OUTPUTS
    Structured key:value output to stdout for log capture.

.NOTES
    Exit codes:
      0 = Installed; service back to Running, extension will load on next start
      3 = Fleet osquery service not present
      4 = Filesystem operation failed (directory, move, loader write, ACL)
      5 = Service did not return to Running after restart
      6 = Download failed
      7 = Downloaded file failed SHA-256 verification
      8 = Unsupported architecture

    Update workflow:
      1. CI rebuilds the binaries on push to main in this repo and publishes
         them to the `latest` release.
      2. After CI publishes, bump $ExtensionVersion and both $Builds entries'
         Sha values to match the new release assets.
      3. Open a PR. On merge, failing hosts pull the new binary on the next
         policy run.

    References:
      Fleet guide: https://fleetdm.com/guides/deploying-custom-osquery-extensions-in-fleet-a-step-by-step-guide
      Compiled default path: osquery's default_paths.h, OSQUERY_HOME for WIN32
        = "\\Program Files\\osquery\\". --extensions_autoload defaults to
        OSQUERY_HOME "extensions.load" in osquery/extensions/extensions.cpp.
#>

[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

# ============================================================================
# Extension identity. Bump $ExtensionVersion and both $Builds[*].Sha values
# together when the published release assets change.
# ============================================================================
$ExtensionName    = 'secureboot_cert_update'
$ExtensionVersion = 'latest'
$BaseUrl          = 'https://github.com/allenhouchins/fleet-extensions/releases/latest/download'

$Builds = @{
    'AMD64' = @{
        Asset = 'secureboot_cert_update-amd64.exe'
        Sha   = 'EAB177637DD5F7E07B6286315C9AA71D6198305B3E9DE98138992424839CF46F'
    }
    'ARM64' = @{
        Asset = 'secureboot_cert_update-arm64.exe'
        Sha   = 'D4C87C5C2906A8630E3145516CFDCF1A8B6F81A1FA1AACB5F43D54ADE91668AE'
    }
}
# ============================================================================

# Match osquery's compiled default on Windows so osqueryd autoloads us when
# orbit does not pass --extensions_autoload (osquery/utils/config/default_paths.h
# defines OSQUERY_HOME as "\\Program Files\\osquery\\" on WIN32).
$OsqueryHome   = 'C:\Program Files\osquery'
$ExtensionsDir = Join-Path $OsqueryHome 'extensions'
$ExtensionPath = Join-Path $ExtensionsDir "$ExtensionName.ext.exe"
$LoaderPath    = Join-Path $OsqueryHome 'extensions.load'
$ServiceName   = 'Fleet osquery'

function Write-State {
    param([string]$Label, [string]$Value)
    Write-Output ("{0,-30} : {1}" -f $Label, $Value)
}

function Set-HardenedAcl {
    # Reset $Path to: owner Administrators, no inherited ACEs, full control for
    # SYSTEM and Administrators, read+execute for Users. Re-asserted on every
    # run so a drifted host (Group Policy churn, manual edits) self-heals.
    # Well-known SIDs are used instead of names so this works on non-English
    # Windows.
    param([string]$Path, [bool]$IsDirectory)

    $systemSid = [System.Security.Principal.SecurityIdentifier]::new('S-1-5-18')
    $adminsSid = [System.Security.Principal.SecurityIdentifier]::new('S-1-5-32-544')
    $usersSid  = [System.Security.Principal.SecurityIdentifier]::new('S-1-5-32-545')

    $acl = Get-Acl -Path $Path
    # Disable inheritance, drop inherited ACEs (false = do not preserve).
    $acl.SetAccessRuleProtection($true, $false)
    foreach ($r in @($acl.Access)) { [void]$acl.RemoveAccessRule($r) }
    $acl.SetOwner($adminsSid)

    $inherit = if ($IsDirectory) {
        [System.Security.AccessControl.InheritanceFlags]'ContainerInherit, ObjectInherit'
    } else {
        [System.Security.AccessControl.InheritanceFlags]::None
    }
    $rules = @(
        [System.Security.AccessControl.FileSystemAccessRule]::new(
            $systemSid, 'FullControl', $inherit, 'None', 'Allow'),
        [System.Security.AccessControl.FileSystemAccessRule]::new(
            $adminsSid, 'FullControl', $inherit, 'None', 'Allow'),
        [System.Security.AccessControl.FileSystemAccessRule]::new(
            $usersSid, 'ReadAndExecute', $inherit, 'None', 'Allow')
    )
    foreach ($r in $rules) { $acl.AddAccessRule($r) }
    Set-Acl -Path $Path -AclObject $acl
}

Write-Output "=== $ExtensionName extension installer ==="
Write-Output ""

# --- Pre-flight ---
if ($null -eq (Get-Service -Name $ServiceName -ErrorAction SilentlyContinue)) {
    Write-Output "FAIL: service '$ServiceName' not found. Is fleetd installed?"
    Write-State 'State' 'service_missing'
    exit 3
}

# --- Architecture selection ---
$arch = $env:PROCESSOR_ARCHITECTURE
if (-not $Builds.ContainsKey($arch)) {
    Write-Output "FAIL: unsupported architecture: $arch"
    Write-State 'State' 'unsupported_arch'
    exit 8
}
$asset       = $Builds[$arch].Asset
$expectedSha = $Builds[$arch].Sha.ToUpper()
$url         = "$BaseUrl/$asset"
Write-State 'Architecture'   $arch
Write-State 'Asset'          $asset
Write-State 'Target'         $ExtensionPath
Write-State 'Loader'         $LoaderPath
Write-State 'Download URL'   $url

# --- Ensure C:\Program Files\osquery\extensions exists, harden it ---
foreach ($dir in @($OsqueryHome, $ExtensionsDir)) {
    if (-not (Test-Path $dir)) {
        try {
            New-Item -ItemType Directory -Path $dir -Force | Out-Null
        } catch {
            Write-Output "FAIL: could not create ${dir}: $($_.Exception.Message)"
            Write-State 'State' 'dir_unwritable'
            exit 4
        }
    }
    try {
        Set-HardenedAcl -Path $dir -IsDirectory $true
    } catch {
        Write-Output "FAIL: could not harden ACL on ${dir}: $($_.Exception.Message)"
        Write-State 'State' 'acl_failed_dir'
        exit 4
    }
}

# --- Idempotent fast-path: skip the download and move when the binary is current ---
# osqueryd holds the extension binary open while loaded, so Move-Item -Force
# fails with "Cannot create a file when that file already exists" against the
# locked destination on a rerun. If the existing file already has the expected
# SHA-256, there is nothing to do; otherwise stop the service before the move
# to release the lock.
$needsPlace = $true
if (Test-Path $ExtensionPath) {
    try {
        $existingSha = (Get-FileHash -Path $ExtensionPath -Algorithm SHA256).Hash.ToUpper()
        if ($existingSha -eq $expectedSha) {
            $needsPlace = $false
            Write-State 'Binary' "already current ($existingSha)"
        } else {
            Write-State 'Binary' "out of date; replacing"
        }
    } catch {
        Write-Output "WARN: could not hash existing ${ExtensionPath}: $($_.Exception.Message)"
    }
}

if ($needsPlace) {
    $tempPath = Join-Path $env:TEMP "$ExtensionName-$([guid]::NewGuid()).download"
    try {
        Invoke-WebRequest -Uri $url -OutFile $tempPath -UseBasicParsing -TimeoutSec 120
    } catch {
        Write-Output "FAIL: download failed: $($_.Exception.Message)"
        Write-Output "      Confirm $url is reachable from the host."
        Write-State 'State' 'download_failed'
        Remove-Item -Path $tempPath -Force -ErrorAction SilentlyContinue
        exit 6
    }
    $actualSha = (Get-FileHash -Path $tempPath -Algorithm SHA256).Hash.ToUpper()
    if ($actualSha -ne $expectedSha) {
        Write-Output "FAIL: SHA-256 mismatch. expected=$expectedSha actual=$actualSha"
        Write-State 'State' 'checksum_mismatch'
        Remove-Item -Path $tempPath -Force -ErrorAction SilentlyContinue
        exit 7
    }
    Write-State 'SHA-256' "ok ($actualSha)"

    # Stop Fleet osquery before the move so osqueryd releases the binary. The
    # final restart at the bottom brings it back up.
    $serviceWasStopped = $false
    if ((Get-Service -Name $ServiceName).Status -eq 'Running') {
        try {
            Stop-Service -Name $ServiceName -Force
            $serviceWasStopped = $true
        } catch {
            Write-Output "FAIL: could not stop ${ServiceName} to replace the binary: $($_.Exception.Message)"
            Write-State 'State' 'stop_failed'
            Remove-Item -Path $tempPath -Force -ErrorAction SilentlyContinue
            exit 5
        }
    }

    # osqueryd does not always tear down its autoloaded extension children
    # when the service stops on Windows, and the extension process keeps the
    # .ext.exe locked. Kill any lingering secureboot_cert_update* process and
    # wait briefly for the file handle to release before the move.
    Get-Process -Name "$ExtensionName*" -ErrorAction SilentlyContinue |
        Stop-Process -Force -ErrorAction SilentlyContinue
    Start-Sleep -Seconds 2

    # Retry the move for a few seconds in case the OS hasn't yet released the
    # handle. On Windows, file-rename-over-existing fails with "Cannot create
    # a file when that file already exists" until every handle drops.
    $moveOk = $false
    $moveErr = $null
    for ($try = 0; $try -lt 5; $try++) {
        try {
            Move-Item -Path $tempPath -Destination $ExtensionPath -Force
            $moveOk = $true
            break
        } catch {
            $moveErr = $_
            Start-Sleep -Seconds 1
        }
    }
    if (-not $moveOk) {
        Write-Output "FAIL: could not move to ${ExtensionPath}: $($moveErr.Exception.Message)"
        Write-State 'State' 'move_failed'
        Remove-Item -Path $tempPath -Force -ErrorAction SilentlyContinue
        if ($serviceWasStopped) { Start-Service -Name $ServiceName -ErrorAction SilentlyContinue }
        exit 4
    }
    Write-State 'Placed' $ExtensionPath
}

try {
    Set-HardenedAcl -Path $ExtensionPath -IsDirectory $false
} catch {
    Write-Output "FAIL: could not harden ACL on ${ExtensionPath}: $($_.Exception.Message)"
    Write-State 'State' 'acl_failed_binary'
    exit 4
}

# --- extensions.load: add our path if missing, write ASCII (no BOM) ---
$loaderChanged = $false
$existing = @()
if (Test-Path $LoaderPath) {
    $existing = @(Get-Content -Path $LoaderPath -ErrorAction SilentlyContinue |
                  Where-Object { $_ -and $_.Trim() -ne '' })
}
if ($existing -notcontains $ExtensionPath) {
    $existing += $ExtensionPath
    try {
        # ASCII no-BOM: a UTF-16 or UTF-8-BOM file makes osquery skip the loader
        # and silently load zero extensions.
        [System.IO.File]::WriteAllLines($LoaderPath, $existing, [System.Text.ASCIIEncoding]::new())
    } catch {
        Write-Output "FAIL: could not write ${LoaderPath}: $($_.Exception.Message)"
        Write-State 'State' 'loader_write_failed'
        exit 4
    }
    Write-State 'extensions.load' 'updated'
    $loaderChanged = $true
} else {
    Write-State 'extensions.load' 'already lists the binary'
}
try {
    Set-HardenedAcl -Path $LoaderPath -IsDirectory $false
} catch {
    Write-Output "FAIL: could not harden ACL on ${LoaderPath}: $($_.Exception.Message)"
    Write-State 'State' 'acl_failed_loader'
    exit 4
}

# --- Bring Fleet osquery back to Running ---
# Start if stopped (we stopped it earlier for the move). Restart if running
# and we changed something. Leave it alone on a true no-op rerun, since the
# extension is already loaded and the policy would not have re-triggered.
$svc = Get-Service -Name $ServiceName
if ($svc.Status -ne 'Running') {
    try {
        Start-Service -Name $ServiceName
    } catch {
        Write-Output "FAIL: could not start ${ServiceName}: $($_.Exception.Message)"
        Write-State 'State' 'start_failed'
        exit 5
    }
    Write-State 'Service' 'started'
} elseif ($needsPlace -or $loaderChanged) {
    try {
        Restart-Service -Name $ServiceName -Force
    } catch {
        Write-Output "FAIL: could not restart ${ServiceName}: $($_.Exception.Message)"
        Write-State 'State' 'restart_failed'
        exit 5
    }
    Write-State 'Service' 'restarted'
} else {
    Write-State 'Service' 'unchanged (no work to do)'
}

Start-Sleep -Seconds 5

$svc = Get-Service -Name $ServiceName
if ($svc.Status -ne 'Running') {
    Write-Output "FAIL: $ServiceName did not return to Running (status: $($svc.Status))."
    Write-State 'State' 'service_not_running'
    exit 5
}

Write-Output ""
Write-Output "OK: installed $ExtensionName $ExtensionVersion at $ExtensionPath."
Write-Output "    osqueryd will autoload the binary via $LoaderPath on the next start."
Write-Output "    Verify in Fleet (live query):"
Write-Output "      SELECT 1 FROM osquery_registry"
Write-Output "        WHERE registry = 'table' AND name = '$ExtensionName' AND active = 1;"
Write-Output "      SELECT state FROM $ExtensionName;"
Write-State 'State' 'installed'
exit 0
