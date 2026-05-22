//go:build windows

// Osquery extension that surfaces a device's progress through Microsoft's
// Secure Boot 2023 certificate rollout. See:
// https://techcommunity.microsoft.com/blog/windows-itpro-blog/secure-boot-playbook-for-certificates-expiring-in-2026/4469235
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/osquery/osquery-go"
	"github.com/osquery/osquery-go/plugin/table"
	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const extensionSchemaVersion = "1.0.0"

// Hard expiration dates from Microsoft's Secure Boot playbook.
var (
	certKEK2011Expires        = time.Date(2026, time.June, 24, 0, 0, 0, 0, time.UTC)
	certUEFICA2011Expires     = time.Date(2026, time.June, 27, 0, 0, 0, 0, time.UTC)
	certWindowsPCA2011Expires = time.Date(2026, time.October, 19, 0, 0, 0, 0, time.UTC)
)

const (
	regSecureBoot          = `SYSTEM\CurrentControlSet\Control\SecureBoot`
	regSecureBootServicing = `SYSTEM\CurrentControlSet\Control\SecureBoot\Servicing`
	regSecureBootState     = `SYSTEM\CurrentControlSet\Control\SecureBoot\State`
	regBIOSInfo            = `HARDWARE\DESCRIPTION\System\BIOS`
	regOSCurrentVersion    = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`

	scheduledTaskPath = `\Microsoft\Windows\PI\Secure-Boot-Update`
)

var (
	socket   = flag.String("socket", "", "Path to the extensions UNIX domain socket")
	timeout  = flag.Int("timeout", 3, "Seconds to wait for autoloaded extensions")
	interval = flag.Int("interval", 3, "Seconds delay between connectivity checks")
)

func main() {
	flag.Parse()
	if *socket == "" {
		log.Fatalln("Missing required --socket argument")
	}

	server, err := osquery.NewExtensionManagerServer(
		"secureboot_cert_update",
		*socket,
		osquery.ServerTimeout(time.Second*time.Duration(*timeout)),
		osquery.ServerPingInterval(time.Second*time.Duration(*interval)),
	)
	if err != nil {
		log.Fatalf("Error creating extension: %s\n", err)
	}

	server.RegisterPlugin(table.NewPlugin(
		"secureboot_cert_update",
		columns(),
		generate,
	))

	if err := server.Run(); err != nil {
		log.Fatal(err)
	}
}

func columns() []table.ColumnDefinition {
	return []table.ColumnDefinition{
		// Derived state
		table.TextColumn("state"),
		table.TextColumn("state_reason"),
		table.IntegerColumn("needs_action"),
		table.TextColumn("action"),
		table.IntegerColumn("days_until_cert_expiry"),

		// Raw playbook signals
		table.IntegerColumn("secureboot_enabled"),
		table.TextColumn("uefica2023_status"),
		table.TextColumn("uefica2023_error"),
		table.TextColumn("available_updates"),
		table.TextColumn("available_updates_policy"),
		table.IntegerColumn("high_confidence_optout"),
		table.IntegerColumn("microsoft_update_managed_optin"),
		table.TextColumn("bucket_hash"),
		table.TextColumn("confidence_level"),
		table.IntegerColumn("windows_uefica2023_capable"),
		table.TextColumn("skip_reason_known_issue"),
		table.TextColumn("can_attempt_update_after"),
		table.TextColumn("secureboot_task_status"),
		table.IntegerColumn("secureboot_task_enabled"),
		table.TextColumn("wincs_key_status"),
		table.IntegerColumn("wincs_key_applied"),
		table.IntegerColumn("reboot_pending"),
		table.IntegerColumn("missing_kek"),
		table.TextColumn("known_issue_id"),
		table.IntegerColumn("latest_event_id"),
		table.TextColumn("latest_event_time"),
		table.TextColumn("last_boot_time"),
		table.TextColumn("collection_time"),

		// Identity
		table.TextColumn("oem_name"),
		table.TextColumn("oem_manufacturer_name"),
		table.TextColumn("oem_model_system_family"),
		table.TextColumn("oem_model_number"),
		table.TextColumn("firmware_manufacturer"),
		table.TextColumn("firmware_version"),
		table.TextColumn("firmware_release_date"),
		table.TextColumn("baseboard_manufacturer"),
		table.TextColumn("baseboard_product"),
		table.TextColumn("os_version"),
		table.TextColumn("os_architecture"),

		// Cert lifecycle
		table.TextColumn("cert_kek_2011_expires_at"),
		table.TextColumn("cert_uefi_ca_2011_expires_at"),
		table.TextColumn("cert_windows_pca_2011_expires_at"),

		table.TextColumn("extension_schema_version"),
	}
}

// collected holds raw values gathered from the device.
type collected struct {
	secureBootEnabled *bool

	uefica2023Status string
	uefica2023Error  string

	availableUpdates            uint32
	availableUpdatesSet         bool
	availableUpdatesPolicy      uint32
	availableUpdatesPolicySet   bool
	highConfidenceOptOut        bool
	microsoftUpdateManagedOptIn bool

	bucketHash                  string
	confidenceLevel             string
	windowsUEFICA2023Capable    *bool
	skipReasonKnownIssue        string
	canAttemptUpdateAfter       string

	wincsKeyStatus  string
	wincsKeyApplied bool

	taskStatus  string
	taskEnabled bool

	rebootPending bool
	missingKEK    bool
	knownIssueID  string

	latestEventID   int
	latestEventTime time.Time

	lastBootTime   time.Time
	collectionTime time.Time

	oemName             string
	oemManufacturer     string
	oemSystemFamily     string
	oemModelNumber      string
	firmwareManufact    string
	firmwareVersion     string
	firmwareReleaseDt   string
	baseboardManufact   string
	baseboardProduct    string
	osVersion           string
	osArchitecture      string
}

func generate(ctx context.Context, q table.QueryContext) ([]map[string]string, error) {
	c := &collected{collectionTime: time.Now().UTC()}

	collectRegistrySecureBoot(c)
	collectRegistryServicing(c)
	collectRegistryDeviceAttributes(c)
	collectRegistryState(c)
	collectRegistryBIOS(c)
	collectRegistryOS(c)
	collectScheduledTask(c)
	collectKernelBootEvents(c)
	collectLastBoot(c)

	state, reason, needsAction, action := deriveState(c)

	row := map[string]string{
		"state":                  state,
		"state_reason":           reason,
		"needs_action":           boolToInt(needsAction),
		"action":                 action,
		"days_until_cert_expiry": strconv.Itoa(daysUntilFirstExpiry(c.collectionTime)),

		"uefica2023_status":              c.uefica2023Status,
		"uefica2023_error":               c.uefica2023Error,
		"available_updates":              uintToHex(c.availableUpdates, c.availableUpdatesSet),
		"available_updates_policy":       uintToHex(c.availableUpdatesPolicy, c.availableUpdatesPolicySet),
		"high_confidence_optout":         boolToInt(c.highConfidenceOptOut),
		"microsoft_update_managed_optin": boolToInt(c.microsoftUpdateManagedOptIn),
		"bucket_hash":                    c.bucketHash,
		"confidence_level":               c.confidenceLevel,
		"skip_reason_known_issue":        c.skipReasonKnownIssue,
		"can_attempt_update_after":       c.canAttemptUpdateAfter,
		"secureboot_task_status":         c.taskStatus,
		"secureboot_task_enabled":        boolToInt(c.taskEnabled),
		"wincs_key_status":               c.wincsKeyStatus,
		"wincs_key_applied":              boolToInt(c.wincsKeyApplied),
		"reboot_pending":                 boolToInt(c.rebootPending),
		"missing_kek":                    boolToInt(c.missingKEK),
		"known_issue_id":                 c.knownIssueID,
		"latest_event_id":                intOrEmpty(c.latestEventID),
		"latest_event_time":              timeOrEmpty(c.latestEventTime),
		"last_boot_time":                 timeOrEmpty(c.lastBootTime),
		"collection_time":                c.collectionTime.Format(time.RFC3339),

		"oem_name":                c.oemName,
		"oem_manufacturer_name":   c.oemManufacturer,
		"oem_model_system_family": c.oemSystemFamily,
		"oem_model_number":        c.oemModelNumber,
		"firmware_manufacturer":   c.firmwareManufact,
		"firmware_version":        c.firmwareVersion,
		"firmware_release_date":   c.firmwareReleaseDt,
		"baseboard_manufacturer":  c.baseboardManufact,
		"baseboard_product":       c.baseboardProduct,
		"os_version":              c.osVersion,
		"os_architecture":         c.osArchitecture,

		"cert_kek_2011_expires_at":          certKEK2011Expires.Format(time.RFC3339),
		"cert_uefi_ca_2011_expires_at":      certUEFICA2011Expires.Format(time.RFC3339),
		"cert_windows_pca_2011_expires_at":  certWindowsPCA2011Expires.Format(time.RFC3339),
		"extension_schema_version":          extensionSchemaVersion,
	}

	if c.secureBootEnabled != nil {
		row["secureboot_enabled"] = boolToInt(*c.secureBootEnabled)
	} else {
		row["secureboot_enabled"] = ""
	}
	if c.windowsUEFICA2023Capable != nil {
		row["windows_uefica2023_capable"] = boolToInt(*c.windowsUEFICA2023Capable)
	} else {
		row["windows_uefica2023_capable"] = ""
	}

	return []map[string]string{row}, nil
}

// --- Collectors ---------------------------------------------------------

func collectRegistrySecureBoot(c *collected) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, regSecureBoot, registry.QUERY_VALUE)
	if err != nil {
		log.Printf("open %s: %v", regSecureBoot, err)
		return
	}
	defer k.Close()

	if v, _, err := k.GetIntegerValue("AvailableUpdates"); err == nil {
		c.availableUpdates = uint32(v)
		c.availableUpdatesSet = true
	}
	if v, _, err := k.GetIntegerValue("AvailableUpdatesPolicy"); err == nil {
		c.availableUpdatesPolicy = uint32(v)
		c.availableUpdatesPolicySet = true
	}
	if v, _, err := k.GetIntegerValue("HighConfidenceOptOut"); err == nil {
		c.highConfidenceOptOut = v != 0
	}
	if v, _, err := k.GetIntegerValue("MicrosoftUpdateManagedOptIn"); err == nil {
		c.microsoftUpdateManagedOptIn = v != 0
	}
}

func collectRegistryServicing(c *collected) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, regSecureBootServicing, registry.QUERY_VALUE)
	if err != nil {
		// Servicing key may not exist on devices that haven't received the rollout payload yet.
		return
	}
	defer k.Close()

	c.uefica2023Status, _, _ = k.GetStringValue("UEFICA2023Status")
	c.uefica2023Error, _, _ = k.GetStringValue("UEFICA2023Error")
	c.bucketHash, _, _ = k.GetStringValue("BucketHash")
	c.confidenceLevel, _, _ = k.GetStringValue("ConfidenceLevel")
	c.skipReasonKnownIssue, _, _ = k.GetStringValue("SkipReasonKnownIssue")
	c.wincsKeyStatus, _, _ = k.GetStringValue("WinCSKeyStatus")
	if v, _, err := k.GetIntegerValue("WinCSKeyApplied"); err == nil {
		c.wincsKeyApplied = v != 0
	}
	if v, _, err := k.GetIntegerValue("WindowsUEFICA2023Capable"); err == nil {
		b := v != 0
		c.windowsUEFICA2023Capable = &b
	}
}

// regSecureBootDeviceAttributes holds the Microsoft-curated OEM identity that
// the Secure Boot servicing stack uses for bucketing, plus the binary
// CanAttemptUpdateAfter FILETIME.
const regSecureBootDeviceAttributes = regSecureBootServicing + `\DeviceAttributes`

func collectRegistryDeviceAttributes(c *collected) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, regSecureBootDeviceAttributes, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()

	c.oemName, _, _ = k.GetStringValue("OEMName")
	c.oemManufacturer, _, _ = k.GetStringValue("OEMManufacturerName")
	c.oemSystemFamily, _, _ = k.GetStringValue("OEMModelSystemFamily")
	c.oemModelNumber, _, _ = k.GetStringValue("OEMModelNumber")
	c.firmwareManufact, _, _ = k.GetStringValue("FirmwareManufacturer")
	c.firmwareVersion, _, _ = k.GetStringValue("FirmwareVersion")
	c.firmwareReleaseDt, _, _ = k.GetStringValue("FirmwareReleaseDate")
	c.baseboardManufact, _, _ = k.GetStringValue("BaseBoardManufacturer")
	c.baseboardProduct, _, _ = k.GetStringValue("OEMModelBaseBoard")
	if v, _, err := k.GetStringValue("OSArchitecture"); err == nil && v != "" {
		c.osArchitecture = v
	}

	if b, _, err := k.GetBinaryValue("CanAttemptUpdateAfter"); err == nil && len(b) > 0 {
		c.canAttemptUpdateAfter = formatCanAttemptUpdateAfter(b)
	}
}

// formatCanAttemptUpdateAfter renders the binary value as an ISO-8601 timestamp
// when it looks like a Windows FILETIME (8 little-endian bytes), or as a hex
// string otherwise so the raw value is still preserved for forensics.
func formatCanAttemptUpdateAfter(b []byte) string {
	if len(b) == 8 {
		var ft uint64
		for i := 0; i < 8; i++ {
			ft |= uint64(b[i]) << (i * 8)
		}
		// FILETIME epoch (1601-01-01 UTC) is 11644473600 seconds before Unix epoch.
		const filetimeUnixDelta int64 = 11644473600
		secs := int64(ft/10000000) - filetimeUnixDelta
		nsec := int64((ft % 10000000) * 100)
		t := time.Unix(secs, nsec).UTC()
		// Sanity-check the result; a FILETIME of 0 or a value far outside a
		// reasonable range means we should fall through to the hex encoding.
		if t.Year() >= 2000 && t.Year() <= 2100 {
			return t.Format(time.RFC3339)
		}
	}
	hexBuf := make([]byte, 0, len(b)*2)
	const hexDigits = "0123456789ABCDEF"
	for _, x := range b {
		hexBuf = append(hexBuf, hexDigits[x>>4], hexDigits[x&0x0F])
	}
	return string(hexBuf)
}

func collectRegistryState(c *collected) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, regSecureBootState, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	if v, _, err := k.GetIntegerValue("UEFISecureBootEnabled"); err == nil {
		b := v != 0
		c.secureBootEnabled = &b
	}
}

// collectRegistryBIOS only fills identity fields that weren't already populated
// by the Servicing\DeviceAttributes subkey — that subkey isn't present on
// devices that haven't received the Secure Boot rollout payload yet, and the
// BIOS subkey is the universal fallback.
func collectRegistryBIOS(c *collected) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, regBIOSInfo, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	fillIfEmpty(&c.oemManufacturer, k, "SystemManufacturer")
	fillIfEmpty(&c.oemSystemFamily, k, "SystemFamily")
	fillIfEmpty(&c.oemModelNumber, k, "SystemProductName")
	fillIfEmpty(&c.firmwareVersion, k, "BIOSVersion")
	fillIfEmpty(&c.firmwareReleaseDt, k, "BIOSReleaseDate")
	fillIfEmpty(&c.baseboardManufact, k, "BaseBoardManufacturer")
	fillIfEmpty(&c.baseboardProduct, k, "BaseBoardProduct")
}

func fillIfEmpty(dst *string, k registry.Key, name string) {
	if *dst != "" {
		return
	}
	if v, _, err := k.GetStringValue(name); err == nil {
		*dst = v
	}
}

func collectRegistryOS(c *collected) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, regOSCurrentVersion, registry.QUERY_VALUE)
	if err != nil {
		return
	}
	defer k.Close()
	productName, _, _ := k.GetStringValue("ProductName")
	displayVersion, _, _ := k.GetStringValue("DisplayVersion")
	buildLab, _, _ := k.GetStringValue("BuildLabEx")
	parts := []string{}
	if productName != "" {
		parts = append(parts, productName)
	}
	if displayVersion != "" {
		parts = append(parts, displayVersion)
	}
	if buildLab != "" {
		parts = append(parts, "build "+buildLab)
	}
	c.osVersion = strings.Join(parts, " ")
	if c.osArchitecture == "" {
		if arch := envProcArch(); arch != "" {
			c.osArchitecture = arch
		}
	}
}

func envProcArch() string {
	// On 64-bit Windows, PROCESSOR_ARCHITECTURE is "AMD64" or "ARM64".
	for _, name := range []string{"PROCESSOR_ARCHITECTURE", "PROCESSOR_ARCHITEW6432"} {
		if v, ok := os.LookupEnv(name); ok && v != "" {
			return v
		}
	}
	return ""
}

func collectScheduledTask(c *collected) {
	cmd := exec.Command("schtasks.exe", "/Query", "/TN", scheduledTaskPath, "/FO", "LIST", "/V")
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Task missing means servicing is broken — leave fields empty so deriveState
		// can detect the gap.
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Status:") {
			c.taskStatus = strings.TrimSpace(strings.TrimPrefix(line, "Status:"))
		}
		if strings.HasPrefix(line, "Scheduled Task State:") {
			v := strings.TrimSpace(strings.TrimPrefix(line, "Scheduled Task State:"))
			c.taskEnabled = strings.EqualFold(v, "Enabled")
		}
	}
}

// kernelBootEvent matches the JSON returned by Get-WinEvent below.
type kernelBootEvent struct {
	ID          int    `json:"Id"`
	TimeCreated string `json:"TimeCreated"`
	Message     string `json:"Message"`
}

var knownIssueRe = regexp.MustCompile(`KI_\d+`)

func collectKernelBootEvents(c *collected) {
	// Query the last 6 months of Kernel-Boot events relevant to Secure Boot servicing.
	psScript := `
$ErrorActionPreference = 'SilentlyContinue'
$start = (Get-Date).AddMonths(-6)
$evts = Get-WinEvent -FilterHashtable @{
    LogName      = 'System'
    ProviderName = 'Microsoft-Windows-Kernel-Boot'
    StartTime    = $start
} -MaxEvents 200 | Where-Object { $_.Id -ge 1795 -and $_.Id -le 1808 }
if (-not $evts) { '[]'; exit }
$evts | ForEach-Object {
    [pscustomobject]@{
        Id          = $_.Id
        TimeCreated = $_.TimeCreated.ToUniversalTime().ToString('o')
        Message     = $_.Message
    }
} | ConvertTo-Json -Compress -Depth 3
`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", psScript)
	out, err := cmd.Output()
	if err != nil {
		log.Printf("get-winevent failed: %v", err)
		return
	}
	body := strings.TrimSpace(string(out))
	if body == "" || body == "[]" {
		return
	}
	// ConvertTo-Json emits an object (not an array) when there's a single event.
	if !strings.HasPrefix(body, "[") {
		body = "[" + body + "]"
	}
	var events []kernelBootEvent
	if err := json.Unmarshal([]byte(body), &events); err != nil {
		log.Printf("parse kernel-boot events: %v", err)
		return
	}
	for _, e := range events {
		switch e.ID {
		case 1803:
			c.missingKEK = true
		case 1802:
			if m := knownIssueRe.FindString(e.Message); m != "" {
				c.knownIssueID = m
			}
		case 1801, 1800:
			c.rebootPending = true
		}
		ts, _ := time.Parse(time.RFC3339Nano, e.TimeCreated)
		if !ts.IsZero() && ts.After(c.latestEventTime) {
			c.latestEventTime = ts
			c.latestEventID = e.ID
		}
	}
}

var (
	kernel32DLL      = windows.NewLazySystemDLL("kernel32.dll")
	procTickCount64  = kernel32DLL.NewProc("GetTickCount64")
)

func collectLastBoot(c *collected) {
	// GetTickCount64 returns milliseconds since boot, monotonically.
	ticks, _, _ := procTickCount64.Call()
	uptime := time.Duration(ticks) * time.Millisecond
	c.lastBootTime = c.collectionTime.Add(-uptime).UTC()
}

// --- State machine ------------------------------------------------------

func deriveState(c *collected) (state, reason string, needsAction bool, action string) {
	// Secure Boot disabled is the most fundamental gap.
	if c.secureBootEnabled != nil && !*c.secureBootEnabled {
		return "SecureBootDisabled",
			"Secure Boot is disabled in UEFI firmware; the 2023 cert rollout will not run until it is enabled",
			true, "enable_secure_boot"
	}

	switch strings.ToLower(c.uefica2023Status) {
	case "updated":
		return "Updated",
			"Device has the 2023 Secure Boot certificates enrolled",
			false, "none"
	}

	if c.missingKEK {
		return "BlockedOEMMissingKEK",
			"Firmware has not supplied a PK-signed KEK update slot (Event 1803). OEM firmware update required.",
			true, "oem_firmware_required"
	}

	if c.knownIssueID != "" {
		return "BlockedKnownIssue",
			fmt.Sprintf("Blocked by Microsoft known issue %s (Event 1802)", c.knownIssueID),
			true, "investigate"
	}

	if c.uefica2023Error != "" {
		return "BlockedFirmwareError",
			fmt.Sprintf("UEFICA2023Error reported: %s", c.uefica2023Error),
			true, "investigate"
	}

	if c.rebootPending {
		return "RebootPending",
			"Update sequence is staged; awaiting reboot to complete",
			false, "reboot"
	}

	if c.availableUpdatesSet && c.availableUpdates != 0 && !strings.EqualFold(c.uefica2023Status, "NotStarted") {
		return "InProgress",
			fmt.Sprintf("Rollout in progress (AvailableUpdates=%s, UEFICA2023Status=%s)",
				uintToHex(c.availableUpdates, true), valueOr(c.uefica2023Status, "unknown")),
			false, "none"
	}

	// Servicing infrastructure broken: task disabled or WinCS key not applied.
	if c.taskStatus != "" && !c.taskEnabled {
		return "ServicingBroken",
			"Scheduled task Secure-Boot-Update is disabled; rollout cannot run",
			true, "investigate"
	}
	if c.wincsKeyStatus != "" && !c.wincsKeyApplied {
		return "ServicingBroken",
			fmt.Sprintf("WinCS key not applied (WinCSKeyStatus=%s); rollout prerequisites incomplete", c.wincsKeyStatus),
			true, "investigate"
	}

	if c.highConfidenceOptOut {
		return "OptedOut",
			"Device opted out of Microsoft's high-confidence rollout (HighConfidenceOptOut=1)",
			false, "apply_manual_override"
	}

	if strings.EqualFold(c.uefica2023Status, "NotStarted") ||
		strings.Contains(strings.ToLower(c.confidenceLevel), "observation") {
		return "WaitingOnRollout",
			fmt.Sprintf("Awaiting phased rollout (ConfidenceLevel=%s, BucketHash=%s)",
				valueOr(c.confidenceLevel, "unknown"), valueOr(c.bucketHash, "unknown")),
			false, "wait"
	}

	// If we got nothing useful at all, surface that.
	if c.uefica2023Status == "" && !c.availableUpdatesSet && c.taskStatus == "" {
		return "Unknown",
			"Could not read Secure Boot servicing state; device may not have received the rollout payload yet",
			false, "wait"
	}

	return "Unknown",
		"State did not match any known case; check raw fields",
		false, "investigate"
}

// --- Helpers ------------------------------------------------------------

func boolToInt(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func intOrEmpty(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

func timeOrEmpty(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func uintToHex(v uint32, set bool) string {
	if !set {
		return ""
	}
	return fmt.Sprintf("0x%04X", v)
}

func valueOr(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func daysUntilFirstExpiry(now time.Time) int {
	earliest := certKEK2011Expires
	if certUEFICA2011Expires.Before(earliest) {
		earliest = certUEFICA2011Expires
	}
	if certWindowsPCA2011Expires.Before(earliest) {
		earliest = certWindowsPCA2011Expires
	}
	delta := earliest.Sub(now)
	return int(delta.Hours() / 24)
}
