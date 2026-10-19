package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"howett.net/plist"
)

func TestParseInfoPlistXML(t *testing.T) {
	data, err := os.ReadFile("testdata/Info.plist")
	if err != nil {
		t.Fatal(err)
	}
	info, err := parseInfoPlist(data)
	if err != nil {
		t.Fatal(err)
	}
	if info.BundleIdentifier != "com.example.editor" || info.BundleShortVersion != "2.4.1" || info.BundleName != "Example Editor" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestParseInfoPlistBinary(t *testing.T) {
	want := InfoPlist{BundleIdentifier: "com.example.bin", BundleVersion: "77", BundleExecutable: "bin"}
	data, err := plist.Marshal(want, plist.BinaryFormat)
	if err != nil {
		t.Fatal(err)
	}
	got, err := parseInfoPlist(data)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if appVersion(got) != "77" {
		t.Fatalf("version fallback to CFBundleVersion failed: %q", appVersion(got))
	}
}

func TestParseInfoPlistInvalid(t *testing.T) {
	if _, err := parseInfoPlist([]byte("not a plist")); err == nil {
		t.Fatal("expected an error for invalid data")
	}
}

func TestAppName(t *testing.T) {
	cases := []struct {
		name string
		info InfoPlist
		path string
		want string
	}{
		{"display name wins", InfoPlist{BundleDisplayName: "Disp", BundleName: "Bun", BundleExecutable: "exe"}, "/Applications/X.app", "Disp"},
		{"bundle name next", InfoPlist{BundleName: "Bun", BundleExecutable: "exe"}, "/Applications/X.app", "Bun"},
		{"executable next", InfoPlist{BundleExecutable: "exe"}, "/Applications/X.app", "exe"},
		{"run.sh is skipped", InfoPlist{BundleExecutable: "run.sh"}, "/Applications/Wrapped.app", "Wrapped"},
		{"folder name last", InfoPlist{}, "/Applications/Some Tool.APP", "Some Tool"},
	}
	for _, c := range cases {
		if got := appName(c.info, c.path); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestParseCodesignAuthority(t *testing.T) {
	data, _ := os.ReadFile("testdata/codesign-devid.txt")
	if got := parseCodesignAuthority(string(data)); got != "Developer ID Application: Example Inc (ABCDE12345)" {
		t.Fatalf("got %q", got)
	}
	unsigned, _ := os.ReadFile("testdata/codesign-unsigned.txt")
	if got := parseCodesignAuthority(string(unsigned)); got != "" {
		t.Fatalf("unsigned: got %q", got)
	}
}

func TestNormalizePublisher(t *testing.T) {
	cases := []struct {
		authority, copyright, want, wantSrc string
	}{
		{"Developer ID Application: Example Inc (ABCDE12345)", "", "Example Inc", "codesign_authority"},
		{"Developer ID Application: Example, Inc. (ABCDE12345)", "Copyright 2020 Other", "Example, Inc.", "codesign_authority"},
		{"3rd Party Mac Developer Application: Store Co (ZZZZZ99999)", "", "Store Co", "codesign_authority"},
		{"Apple Distribution: Dist Co (YYYYY88888)", "", "Dist Co", "codesign_authority"},
		{"Software Signing", "Copyright © 2026 Apple Inc. All rights reserved.", "Apple", "apple"},
		{"Apple Code Signing Certification Authority", "", "Apple", "apple"},
		{"Apple Mac OS Application Signing", "Copyright © 2024 Store Vendor LLC. All rights reserved.", "Store Vendor LLC.", "copyright"},
		{"Apple Mac OS Application Signing", "", "", ""},
		{"", "© 2019-2025 Unsigned Maker", "Unsigned Maker", "copyright"},
		{"", "", "", ""},
		{"Some Other CA", "", "Some Other CA", "codesign_authority"},
	}
	for _, c := range cases {
		got, src := normalizePublisher(c.authority, c.copyright)
		if got != c.want || src != c.wantSrc {
			t.Errorf("normalizePublisher(%q, %q) = %q, %q; want %q, %q", c.authority, c.copyright, got, src, c.want, c.wantSrc)
		}
	}
}

func TestParseCopyright(t *testing.T) {
	cases := map[string]string{
		"Copyright © 2024 Example Inc. All rights reserved.": "Example Inc.",
		"© 2010-2024 Foo LLC":                                "Foo LLC",
		"Copyright 2020, Bar Corp.":                          "Bar Corp.",
		"(c) 2023 Baz GmbH\nSecond line":                     "Baz GmbH",
		"Copyright (C) 2001–present Qux Ltd":                 "Qux Ltd",
		"All rights reserved.":                               "",
		"":                                                   "",
		"Copyright 2024":                                     "",
	}
	for in, want := range cases {
		if got := parseCopyright(in); got != want {
			t.Errorf("parseCopyright(%q) = %q, want %q", in, got, want)
		}
	}
	long := "Copyright 2024 " + strings.Repeat("Word ", 30)
	if got := parseCopyright(long); got != "" {
		t.Errorf("over-long copyright should be dropped, got %q", got)
	}
}

func TestInstallHistory(t *testing.T) {
	data, err := os.ReadFile("testdata/InstallHistory.plist")
	if err != nil {
		t.Fatal(err)
	}
	entries, err := parseInstallHistory(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("got %d entries", len(entries))
	}
	idx := historyIndex(entries)
	want := time.Date(2025, 1, 15, 8, 30, 0, 0, time.UTC).Unix()
	if got := matchInstallHistory(idx, "EXAMPLE EDITOR"); got != want {
		t.Fatalf("earliest, case-insensitive match: got %d, want %d", got, want)
	}
	if got := matchInstallHistory(idx, "", "nope", "Example Editor "); got != want {
		t.Fatalf("second name should match: got %d", got)
	}
	if got := matchInstallHistory(idx, "Missing App"); got != 0 {
		t.Fatalf("no match should be 0, got %d", got)
	}
	if _, ok := idx[""]; ok {
		t.Fatal("entries without a display name must be skipped")
	}
}

func TestParseMdlsOutput(t *testing.T) {
	out := []byte("2026-03-01 10:00:00 +0000\x00(null)\x002025-12-31 23:00:00 -0500\x00")
	got := parseMdlsOutput(out, 4)
	want := []int64{
		time.Date(2026, 3, 1, 10, 0, 0, 0, time.UTC).Unix(),
		0,
		time.Date(2026, 1, 1, 4, 0, 0, 0, time.UTC).Unix(),
		0,
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %d, want %d", i, got[i], want[i])
		}
	}
	if got := parseMdlsOutput([]byte("garbage"), 1); got[0] != 0 {
		t.Errorf("garbage should be 0, got %d", got[0])
	}
}
