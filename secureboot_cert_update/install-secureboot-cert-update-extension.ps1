<#
.SYNOPSIS
    Installs and loads the secureboot_cert_update osquery extension on this host.

.DESCRIPTION
    Downloads the architecture-matching binary from this repo's latest
    release, places it under C:\Program Files\osquery\extensions\, adds the
    path to C:\Program Files\osquery\extensions.load, and restarts the
    Fleet osquery service. Idempotent: rerunning re-asserts the loader and
    ACLs.

    Pattern mirrors the Linux installers in this repo (snap_packages,
    ubuntu_pro) adapted for Windows: no SHA pinning in source, always pull
    the latest release. The CI workflow republishes binaries on push to
    main, so the install script never needs touching.

    Windows specifics not present in the Linux versions:
      - Stops the Fleet osquery service and kills the lingering extension
        child before overwriting the .exe (Windows holds file locks on
        loaded modules).
      - Hardens the ACL on the binary, extensions directory, and loader to
        owner Administrators, full control for SYSTEM + Administrators,
        read+execute for Users. Re-asserted on every run.
      - Writes the loader file as ASCII with no BOM; a UTF-16 or UTF-8-BOM
        loader makes osquery silently skip every entry.

    Writes to osquery's compiled-in default autoload path on Windows
    (C:\Program Files\osquery\extensions.load) so osqueryd autoloads the
    extension when orbit does not pass --extensions_autoload. Twin of the
    Linux installers that write to /etc/osquery/extensions.load.

.OUTPUTS
    Structured key:value output to stdout.

.NOTES
    Exit codes:
      0 = Installed; service back to Running
      3 = Fleet osquery service not present
      4 = Filesystem operation failed (directory, move, loader write, ACL)
      5 = Service did not return to Running after restart
      6 = Download failed or asset is not a valid PE32+ executable
      8 = Unsupported architecture
#>

[CmdletBinding()]
param()

$ErrorActionPreference = 'Stop'

# ============================================================================
# Extension identity. Binaries are produced by this repo's CI on push to main
# and attached to the `latest` release; this script always pulls from there,
# so no version or hash needs bumping in source when CI republishes.
# ============================================================================
$ExtensionName = 'secureboot_cert_update'
$GithubRepo    = 'allenhouchins/fleet-extensions'
$BaseUrl       = "https://github.com/$GithubRepo/releases/latest/download"

$Assets = @{
    'AMD64' = 'secureboot_cert_update-amd64.exe'
    'ARM64' = 'secureboot_cert_update-arm64.exe'
}
# ============================================================================

# Match osquery's compiled default on Windows so osqueryd autoloads us when
# orbit does not pass --extensions_autoload.
$OsqueryHome   = 'C:\Program Files\osquery'
$ExtensionsDir = Join-Path $OsqueryHome 'extensions'
$ExtensionPath = Join-Path $ExtensionsDir "$ExtensionName.ext.exe"
$BackupPath    = "$ExtensionPath.backup.$(Get-Date -Format 'yyyyMMdd_HHmmss')"
$LoaderPath    = Join-Path $OsqueryHome 'extensions.load'
$ServiceName   = 'Fleet osquery'

function Write-State {
    param([string]$Label, [string]$Value)
    Write-Output ("{0,-30} : {1}" -f $Label, $Value)
}

function Set-HardenedAcl {
    # Owner Administrators, no inherited ACEs, full control for SYSTEM and
    # Administrators, read+execute for Users. Well-known SIDs so this works
    # on non-English Windows.
    param([string]$Path, [bool]$IsDirectory)

    $systemSid = [System.Security.Principal.SecurityIdentifier]::new('S-1-5-18')
    $adminsSid = [System.Security.Principal.SecurityIdentifier]::new('S-1-5-32-544')
    $usersSid  = [System.Security.Principal.SecurityIdentifier]::new('S-1-5-32-545')

    $acl = Get-Acl -Path $Path
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

function Test-WindowsExecutable {
    # Sanity check: file is non-empty and starts with the MZ DOS stub that
    # every PE32+ binary opens with. Cheap, catches HTML 404 pages and
    # truncated downloads.
    param([string]$Path)

    if (-not (Test-Path $Path)) { return $false }
    if ((Get-Item $Path).Length -lt 1024) { return $false }
    $bytes = [byte[]]::new(2)
    $stream = [System.IO.File]::OpenRead($Path)
    try {
        [void]$stream.Read($bytes, 0, 2)
    } finally {
        $stream.Dispose()
    }
    return ($bytes[0] -eq 0x4D -and $bytes[1] -eq 0x5A)
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
if (-not $Assets.ContainsKey($arch)) {
    Write-Output "FAIL: unsupported architecture: $arch"
    Write-State 'State' 'unsupported_arch'
    exit 8
}
$asset = $Assets[$arch]
$url   = "$BaseUrl/$asset"
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

# --- Backup existing binary so we can restore on failure ---
$hadExisting = $false
if (Test-Path $ExtensionPath) {
    try {
        Copy-Item -Path $ExtensionPath -Destination $BackupPath -Force
        $hadExisting = $true
        Write-State 'Backup' $BackupPath
    } catch {
        Write-Output "WARN: could not back up ${ExtensionPath}: $($_.Exception.Message)"
    }
}

# --- Download to a temp path, sanity-check the bytes ---
$tempPath = Join-Path $env:TEMP "$ExtensionName-$([guid]::NewGuid()).download"
try {
    Invoke-WebRequest -Uri $url -OutFile $tempPath -UseBasicParsing -TimeoutSec 120
} catch {
    Write-Output "FAIL: download failed: $($_.Exception.Message)"
    Write-Output "      Confirm $url is reachable from the host."
    Write-State 'State' 'download_failed'
    Remove-Item -Path $tempPath -Force -ErrorAction SilentlyContinue
    Remove-Item -Path $BackupPath -Force -ErrorAction SilentlyContinue
    exit 6
}
if (-not (Test-WindowsExecutable -Path $tempPath)) {
    Write-Output "FAIL: downloaded file is not a Windows PE executable (MZ header missing or file too small)."
    Write-Output "      Confirm the release at https://github.com/$GithubRepo/releases/latest has $asset."
    Write-State 'State' 'invalid_payload'
    Remove-Item -Path $tempPath -Force -ErrorAction SilentlyContinue
    Remove-Item -Path $BackupPath -Force -ErrorAction SilentlyContinue
    exit 6
}
Write-State 'Validated' 'MZ header ok'

# --- Stop service, kill lingering child, move into place ---
$serviceWasStopped = $false
if ((Get-Service -Name $ServiceName).Status -eq 'Running') {
    try {
        Stop-Service -Name $ServiceName -Force
        $serviceWasStopped = $true
    } catch {
        Write-Output "FAIL: could not stop ${ServiceName}: $($_.Exception.Message)"
        Write-State 'State' 'stop_failed'
        Remove-Item -Path $tempPath -Force -ErrorAction SilentlyContinue
        exit 5
    }
}

# osqueryd does not always tear down its autoloaded extension children when
# the service stops on Windows, and the extension process keeps the
# .ext.exe locked. Kill any lingering process and wait briefly for the
# handle to release.
Get-Process -Name "$ExtensionName*" -ErrorAction SilentlyContinue |
    Stop-Process -Force -ErrorAction SilentlyContinue
Start-Sleep -Seconds 2

# Retry the move for a few seconds in case the OS hasn't released the
# handle. File-rename-over-existing fails with "Cannot create a file when
# that file already exists" until every handle drops.
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
    if ($hadExisting -and (Test-Path $BackupPath)) {
        Move-Item -Path $BackupPath -Destination $ExtensionPath -Force -ErrorAction SilentlyContinue
        Write-Output "      Restored previous binary from backup."
    }
    if ($serviceWasStopped) { Start-Service -Name $ServiceName -ErrorAction SilentlyContinue }
    exit 4
}
Write-State 'Placed' $ExtensionPath

try {
    Set-HardenedAcl -Path $ExtensionPath -IsDirectory $false
} catch {
    Write-Output "FAIL: could not harden ACL on ${ExtensionPath}: $($_.Exception.Message)"
    Write-State 'State' 'acl_failed_binary'
    exit 4
}

# --- extensions.load: add our path if missing, write ASCII no-BOM ---
$loaderChanged = $false
$existing = @()
if (Test-Path $LoaderPath) {
    $existing = @(Get-Content -Path $LoaderPath -ErrorAction SilentlyContinue |
                  Where-Object { $_ -and $_.Trim() -ne '' })
}
if ($existing -notcontains $ExtensionPath) {
    $existing += $ExtensionPath
    try {
        # ASCII no-BOM: a UTF-16 or UTF-8-BOM file makes osquery skip the
        # loader and silently load zero extensions.
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
} elseif ($loaderChanged) {
    try {
        Restart-Service -Name $ServiceName -Force
    } catch {
        Write-Output "FAIL: could not restart ${ServiceName}: $($_.Exception.Message)"
        Write-State 'State' 'restart_failed'
        exit 5
    }
    Write-State 'Service' 'restarted'
} else {
    Write-State 'Service' 'running'
}

Start-Sleep -Seconds 5

$svc = Get-Service -Name $ServiceName
if ($svc.Status -ne 'Running') {
    Write-Output "FAIL: $ServiceName did not return to Running (status: $($svc.Status))."
    Write-State 'State' 'service_not_running'
    exit 5
}

# Clean up the backup now that the install succeeded.
if (Test-Path $BackupPath) {
    Remove-Item -Path $BackupPath -Force -ErrorAction SilentlyContinue
}

Write-Output ""
Write-Output "OK: installed $ExtensionName at $ExtensionPath."
Write-Output "    osqueryd will autoload the binary via $LoaderPath on the next start."
Write-Output "    Verify in Fleet (live query):"
Write-Output "      SELECT 1 FROM osquery_registry"
Write-Output "        WHERE registry = 'table' AND name = '$ExtensionName' AND active = 1;"
Write-Output "      SELECT state FROM $ExtensionName;"
Write-State 'State' 'installed'
exit 0
