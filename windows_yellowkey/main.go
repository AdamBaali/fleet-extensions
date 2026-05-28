//go:build windows

// Package main implements an osquery extension for the YellowKey BitLocker
// bypass (CVE-2026-45585). It registers one table, windows_yellowkey, that
// returns a single-row per-host verdict. The matching fix is
// mitigate-windows-yellowkey.ps1 in this repo, which applies Microsoft's
// autofstx strip from the CVE-2026-45585 MSRC advisory FAQ.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/osquery/osquery-go"
	"github.com/osquery/osquery-go/plugin/table"
	"golang.org/x/sys/windows/registry"
)

const tableName = "windows_yellowkey"

// Absolute paths to the native tools we shell out to. osqueryd's SYSTEM child
// environment does not always include System32 on PATH, and Go's os/exec
// (since 1.19) refuses to run an executable it can only find relative to the
// current directory. Using %SystemRoot%\System32 explicitly sidesteps both.
var (
	reagentcExe   = systemRootJoin("System32", "reagentc.exe")
	powershellExe = systemRootJoin("System32", "WindowsPowerShell", "v1.0", "powershell.exe")
)

func systemRootJoin(elem ...string) string {
	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}
	return filepath.Join(append([]string{root}, elem...)...)
}

// Verdicts emitted in the state column. First match in verdict() wins.
const (
	stateNotAffected    = "not_affected"
	stateMitigated      = "mitigated"
	stateMitigatedWinRE = "mitigated_winre_off"
	stateBitLockerOff   = "bitlocker_off"
	stateExposed        = "exposed"
)

// WinRE states returned by winreState().
const (
	winreEnabled  = "Enabled"
	winreDisabled = "Disabled"
	winreUnknown  = "unknown"
)

// BitLocker key protector type names from Get-BitLockerVolume.
const (
	kpTPM              = "Tpm"
	kpTPMPin           = "TpmPin"
	kpTPMPinStartupKey = "TpmPinStartupKey"
)

func main() {
	socket := flag.String("socket", "", "path to the osquery extension socket")
	timeout := flag.Int("timeout", 3, "seconds to wait for autoloaded extensions")
	interval := flag.Int("interval", 3, "seconds between connectivity checks")
	// osqueryd forwards --verbose to autoloaded extensions whenever it runs
	// verbose. flag.Parse uses ExitOnError, so an undefined flag makes the
	// process exit before it registers the table. Accept the flag to stay
	// loaded under verbose osquery.
	_ = flag.Bool("verbose", false, "accept osqueryd's verbose flag")
	flag.Parse()

	if *socket == "" {
		log.Fatal("missing required --socket argument")
	}

	server, err := osquery.NewExtensionManagerServer(
		tableName, *socket,
		osquery.ServerTimeout(time.Duration(*timeout)*time.Second),
		osquery.ServerPingInterval(time.Duration(*interval)*time.Second),
	)
	if err != nil {
		log.Fatalf("create extension server: %v", err)
	}

	server.RegisterPlugin(table.NewPlugin(tableName, columns(), generate))

	if err := server.Run(); err != nil {
		log.Fatalf("run extension server: %v", err)
	}
}

func columns() []table.ColumnDefinition {
	return []table.ColumnDefinition{
		table.TextColumn("state"),
		table.TextColumn("state_reason"),
		table.IntegerColumn("needs_action"),
		table.TextColumn("winre_enabled"),
		table.IntegerColumn("tpm_only"),
		table.IntegerColumn("mitigated"),
	}
}

// generate returns one row: the host's YellowKey verdict and the signals
// behind it. Collection failures are logged and fall back to their safe
// defaults so the table always returns a row.
func generate(_ context.Context, _ table.QueryContext) ([]map[string]string, error) {
	affected := osAffected()
	winre := winreState()
	protected, tpmOnly := bitLockerState()
	mit := mitigated()

	state, reason := verdict(affected, winre, protected, mit)

	return []map[string]string{{
		"state":         state,
		"state_reason":  reason,
		"needs_action":  boolToInt(state == stateExposed),
		"winre_enabled": winre,
		"tpm_only":      boolToInt(tpmOnly),
		"mitigated":     boolToInt(mit),
	}}, nil
}

// verdict derives the per-host state. First match wins.
func verdict(affected bool, winre string, protected, mitigated bool) (state, reason string) {
	switch {
	case !affected:
		return stateNotAffected, "Windows 10 or unrecognised SKU; not vulnerable"
	case mitigated:
		return stateMitigated, "autofstx stripped from WinRE (BootExecMitigated marker set)"
	case winre == winreDisabled:
		return stateMitigatedWinRE, "WinRE disabled; the bypass cannot run"
	case !protected:
		return stateBitLockerOff, "BitLocker is not protecting any volume"
	case winre == winreUnknown:
		return stateExposed, "BitLocker on, WinRE state unknown (assumed on), no mitigation applied"
	default:
		return stateExposed, "WinRE on, BitLocker on, no mitigation applied"
	}
}

// osAffected reports whether this host runs an OS that YellowKey affects:
// Windows 11, Server 2022, or Server 2025. Windows 10 ships a different
// WinRE component and is safe.
//
// Windows 11's registry ProductName still reads "Windows 10 ..." (Microsoft
// never updated it after Windows 11 shipped), so client SKUs are distinguished
// by CurrentBuild instead: Windows 11 starts at build 22000. Server SKUs do
// label themselves correctly in ProductName, so server detection uses the name.
func osAffected() bool {
	const key = `SOFTWARE\Microsoft\Windows NT\CurrentVersion`
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, key, registry.QUERY_VALUE)
	if err != nil {
		log.Printf("open %s: %v", key, err)
		return false
	}
	defer k.Close()

	name, _, err := k.GetStringValue("ProductName")
	if err != nil {
		log.Printf("read ProductName: %v", err)
		return false
	}
	name = strings.ToLower(name)

	if strings.Contains(name, "server") {
		return strings.Contains(name, "2022") || strings.Contains(name, "2025")
	}

	// Client SKU: ProductName misreports Windows 11 as "Windows 10", so use
	// the build number. Windows 10 client builds max out below 22000.
	buildStr, _, err := k.GetStringValue("CurrentBuild")
	if err != nil {
		log.Printf("read CurrentBuild: %v", err)
		return false
	}
	build, err := strconv.Atoi(strings.TrimSpace(buildStr))
	if err != nil {
		log.Printf("parse CurrentBuild %q: %v", buildStr, err)
		return false
	}
	return build >= 22000
}

// Plain word-boundary matches on the English status words. reagentc /info
// only emits these in the WinRE status line, so a bare \bEnabled\b /
// \bDisabled\b search is reliable and tolerates layout changes (extra
// whitespace, tabs, different label wording across Windows builds) that the
// previous colon-prefixed pattern was too strict for.
var (
	reEnabled  = regexp.MustCompile(`\bEnabled\b`)
	reDisabled = regexp.MustCompile(`\bDisabled\b`)
)

// winreState returns winreEnabled, winreDisabled, or winreUnknown by parsing
// reagentc /info. The status words are English-only; an unrecognised locale
// yields winreUnknown.
//
// The detection runs reagentc directly first (fast path); if the output does
// not contain Enabled/Disabled (some Windows builds emit text whose console
// encoding the direct child-process capture mangles), it falls back to running
// reagentc through PowerShell, which normalizes the bytes via .NET strings.
func winreState() string {
	if s := winreStateFromCmd(reagentcExe, "/info"); s != winreUnknown {
		return s
	}
	return winreStateFromCmd(powershellExe,
		"-NoProfile", "-NonInteractive", "-Command",
		"& '"+reagentcExe+"' /info 2>&1 | Out-String")
}

func winreStateFromCmd(name string, args ...string) string {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		// reagentc exits non-zero in some states but still prints useful text,
		// so parse what we captured rather than bailing.
		log.Printf("winre probe %s %v: %v", name, args, err)
	}
	// Strip null bytes so this tolerates a UTF-16-LE emit (which collapses to
	// ASCII once the nulls go) without a full Unicode decode dance.
	text := strings.ReplaceAll(string(out), "\x00", "")
	switch {
	case reDisabled.MatchString(text):
		return winreDisabled
	case reEnabled.MatchString(text):
		return winreEnabled
	default:
		if strings.TrimSpace(text) != "" {
			log.Printf("winre probe %s did not match Enabled/Disabled; first 200 bytes: %.200q", name, text)
		}
		return winreUnknown
	}
}

// bitLockerState reports whether any volume is protected, and whether any
// volume uses TPM without a PIN (the configuration the published PoC
// targets). Both default to false when BitLocker data is unavailable.
func bitLockerState() (protected, tpmOnly bool) {
	const script = `
$ErrorActionPreference='SilentlyContinue'
$v = Get-BitLockerVolume
if (-not $v) { '[]'; exit }
$v | ForEach-Object {
  [pscustomobject]@{
    On  = [int]($_.ProtectionStatus -eq 'On')
    KPs = @($_.KeyProtector | ForEach-Object { $_.KeyProtectorType.ToString() })
  }
} | ConvertTo-Json -Compress -Depth 4`

	out, err := exec.Command(powershellExe, "-NoProfile", "-NonInteractive", "-Command", script).Output()
	if err != nil {
		log.Printf("Get-BitLockerVolume: %v", err)
		return false, false
	}
	body := strings.TrimSpace(string(out))
	if body == "" || body == "[]" {
		return false, false
	}
	if !strings.HasPrefix(body, "[") {
		body = "[" + body + "]" // ConvertTo-Json emits a bare object for a single volume
	}

	var volumes []struct {
		On         int      `json:"On"`
		Protectors []string `json:"KPs"`
	}
	if err := json.Unmarshal([]byte(body), &volumes); err != nil {
		log.Printf("parse Get-BitLockerVolume output: %v", err)
		return false, false
	}
	for _, v := range volumes {
		if v.On == 1 {
			protected = true
		}
		if tpmWithoutPin(v.Protectors) {
			tpmOnly = true
		}
	}
	return protected, tpmOnly
}

// tpmWithoutPin reports whether a key protector set includes a TPM protector
// but no PIN.
func tpmWithoutPin(protectors []string) bool {
	tpm, pin := false, false
	for _, p := range protectors {
		switch p {
		case kpTPM:
			tpm = true
		case kpTPMPin, kpTPMPinStartupKey:
			pin = true
		}
	}
	return tpm && !pin
}

// mitigated reports whether mitigate-windows-yellowkey.ps1 has recorded a
// successful autofstx strip. A missing Fleet key is the normal
// pre-mitigation state, not an error.
func mitigated() bool {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SOFTWARE\Fleet\YellowKey`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("BootExecMitigated")
	return err == nil && v == 1
}

// boolToInt renders a bool as the "1"/"0" string osquery integer columns use.
func boolToInt(b bool) string {
	if b {
		return "1"
	}
	return "0"
}
