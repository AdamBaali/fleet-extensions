package main

import (
	"bytes"
	"regexp"
	"strings"
	"time"

	"howett.net/plist"
)

// These parsers are kept free of I/O and build tags so they're unit tested on
// any OS. The darwin collector feeds them file contents and command output.

// InfoPlist holds the Info.plist keys the table uses.
type InfoPlist struct {
	BundleIdentifier         string `plist:"CFBundleIdentifier"`
	BundleShortVersion       string `plist:"CFBundleShortVersionString"`
	BundleVersion            string `plist:"CFBundleVersion"`
	BundleDisplayName        string `plist:"CFBundleDisplayName"`
	BundleName               string `plist:"CFBundleName"`
	BundleExecutable         string `plist:"CFBundleExecutable"`
	NSHumanReadableCopyright string `plist:"NSHumanReadableCopyright"`
}

func parseInfoPlist(data []byte) (InfoPlist, error) {
	var info InfoPlist
	_, err := plist.Unmarshal(data, &info)
	return info, err
}

// appName follows Fleet's software_macos query, so rows join on name as well
// as on path: display name, bundle name, executable (unless it's a run.sh
// wrapper), then the folder name without ".app".
func appName(info InfoPlist, bundlePath string) string {
	for _, v := range []string{info.BundleDisplayName, info.BundleName} {
		if v != "" {
			return v
		}
	}
	if info.BundleExecutable != "" && info.BundleExecutable != "run.sh" {
		return info.BundleExecutable
	}
	base := bundlePath
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	if strings.HasSuffix(strings.ToLower(base), ".app") {
		base = base[:len(base)-4]
	}
	return base
}

func appVersion(info InfoPlist) string {
	if info.BundleShortVersion != "" {
		return info.BundleShortVersion
	}
	return info.BundleVersion
}

// parseCodesignAuthority returns the first Authority line from
// `codesign -dvv` output. The first line is the leaf certificate, which names
// the signer; the later lines are its issuers.
func parseCodesignAuthority(out string) string {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Authority=") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Authority="))
		}
	}
	return ""
}

var teamIDSuffix = regexp.MustCompile(`\s*\([A-Z0-9]{10}\)$`)

// Certificate prefixes that name the company that signed the app.
var signerPrefixes = []string{
	"Developer ID Application: ",
	"3rd Party Mac Developer Application: ",
	"Apple Distribution: ",
}

// normalizePublisher turns a signing authority into a publisher name. App
// Store apps are re-signed by Apple, so their authority says nothing about the
// publisher and the copyright string is the only native hint left.
func normalizePublisher(authority, copyright string) (string, string) {
	for _, p := range signerPrefixes {
		if strings.HasPrefix(authority, p) {
			return teamIDSuffix.ReplaceAllString(strings.TrimPrefix(authority, p), ""), "codesign_authority"
		}
	}
	if authority == "Software Signing" || strings.HasPrefix(authority, "Apple Code Signing Certification Authority") {
		return "Apple", "apple"
	}
	if c := parseCopyright(copyright); c != "" {
		return c, "copyright"
	}
	if authority != "" && authority != "Apple Mac OS Application Signing" {
		return authority, "codesign_authority"
	}
	return "", ""
}

var (
	copyrightWords  = regexp.MustCompile(`(?i)copyright|©|\(c\)`)
	copyrightYears  = regexp.MustCompile(`\b(19|20)\d{2}(\s*[-–]\s*((19|20)\d{2}|present))?\b`)
	rightsReserved  = regexp.MustCompile(`(?i)all rights reserved.*$`)
	leadingJunk     = regexp.MustCompile(`^[\s,.;:-]+`)
	trailingJunk    = regexp.MustCompile(`[\s,;:-]+$`)
	collapseSpaces  = regexp.MustCompile(`\s+`)
	maxPublisherLen = 100
)

// parseCopyright pulls a company name out of NSHumanReadableCopyright, for
// example "Copyright © 2024 Example Inc. All rights reserved." -> "Example Inc.".
func parseCopyright(s string) string {
	s = strings.SplitN(s, "\n", 2)[0]
	s = rightsReserved.ReplaceAllString(s, "")
	s = copyrightWords.ReplaceAllString(s, " ")
	s = copyrightYears.ReplaceAllString(s, " ")
	s = collapseSpaces.ReplaceAllString(s, " ")
	s = leadingJunk.ReplaceAllString(s, "")
	s = trailingJunk.ReplaceAllString(s, "")
	if s == "" || len(s) > maxPublisherLen {
		return ""
	}
	return s
}

// HistoryEntry is one install in /Library/Receipts/InstallHistory.plist.
type HistoryEntry struct {
	Date        time.Time `plist:"date"`
	DisplayName string    `plist:"displayName"`
	Version     string    `plist:"displayVersion"`
	ProcessName string    `plist:"processName"`
}

func parseInstallHistory(data []byte) ([]HistoryEntry, error) {
	var entries []HistoryEntry
	_, err := plist.Unmarshal(data, &entries)
	return entries, err
}

// historyIndex maps a lowercased display name to its earliest install, which
// is the first install even when later updates added more entries.
func historyIndex(entries []HistoryEntry) map[string]int64 {
	idx := map[string]int64{}
	for _, e := range entries {
		if e.DisplayName == "" || e.Date.IsZero() {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(e.DisplayName))
		u := e.Date.Unix()
		if cur, ok := idx[k]; !ok || u < cur {
			idx[k] = u
		}
	}
	return idx
}

// matchInstallHistory returns the earliest install for any of the names.
func matchInstallHistory(idx map[string]int64, names ...string) int64 {
	var best int64
	for _, n := range names {
		if n == "" {
			continue
		}
		if u, ok := idx[strings.ToLower(strings.TrimSpace(n))]; ok && (best == 0 || u < best) {
			best = u
		}
	}
	return best
}

// mdlsTimeLayout is how mdls prints dates in -raw mode.
const mdlsTimeLayout = "2006-01-02 15:04:05 -0700"

// parseMdlsOutput splits `mdls -raw` output for n files. mdls separates
// values with NUL and prints "(null)" for files without the attribute.
func parseMdlsOutput(out []byte, n int) []int64 {
	res := make([]int64, n)
	parts := bytes.Split(out, []byte{0})
	for i := 0; i < n && i < len(parts); i++ {
		v := strings.TrimSpace(string(parts[i]))
		if v == "" || v == "(null)" {
			continue
		}
		if t, err := time.Parse(mdlsTimeLayout, v); err == nil {
			res[i] = t.Unix()
		}
	}
	return res
}
