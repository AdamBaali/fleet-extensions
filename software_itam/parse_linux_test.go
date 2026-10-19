package main

import (
	"compress/gzip"
	"os"
	"testing"
	"time"
)

func TestParseRPMOutput(t *testing.T) {
	out := "bash\t5.2.26\t3.fc40\tx86_64\tFedora Project\t1792290230\n" +
		"gpg-pubkey\ta15b79cc\t63d04c2c\t(none)\t(none)\t1792290000\n" +
		"broken line\n" +
		"\n"
	items := parseRPMOutput(out)
	if len(items) != 2 {
		t.Fatalf("got %d items", len(items))
	}
	b := items[0]
	if b.Source != "rpm_packages" || b.Name != "bash" || b.Version != "5.2.26" || b.Arch != "x86_64" ||
		b.Publisher != "Fedora Project" || b.PublisherSource != "rpm_vendor" {
		t.Fatalf("unexpected bash item: %+v", b)
	}
	if d, src := pickInstallDate(b); d != 1792290230 || src != "rpm_install_time" {
		t.Fatalf("install date %d %q", d, src)
	}
	if items[1].Publisher != "" || items[1].PublisherSource != "" {
		t.Fatalf("(none) vendor should be empty: %+v", items[1])
	}
}

func TestParseDpkgStatus(t *testing.T) {
	f, err := os.Open("testdata/dpkg-status")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pkgs, err := parseDpkgStatus(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(pkgs) != 3 {
		t.Fatalf("want 3 installed packages, got %d: %+v", len(pkgs), pkgs)
	}
	for _, p := range pkgs {
		if p.Name == "removed-pkg" {
			t.Fatal("non-installed package was kept")
		}
	}
	if pkgs[1].Name != "libc6" || pkgs[1].Arch != "amd64" || pkgs[1].Version != "2.39-0ubuntu8" ||
		pkgs[1].Maintainer != "Ubuntu Developers <ubuntu-devel-discuss@lists.ubuntu.com>" {
		t.Fatalf("unexpected libc6: %+v", pkgs[1])
	}
}

func TestParseDpkgLogEarliestWins(t *testing.T) {
	loc := time.UTC
	m := map[string]int64{}
	f, err := os.Open("testdata/dpkg.log")
	if err != nil {
		t.Fatal(err)
	}
	if err := parseDpkgLog(f, loc, m); err != nil {
		t.Fatal(err)
	}
	f.Close()

	gzf, err := os.Open("testdata/dpkg.log.2.gz")
	if err != nil {
		t.Fatal(err)
	}
	gz, err := gzip.NewReader(gzf)
	if err != nil {
		t.Fatal(err)
	}
	if err := parseDpkgLog(gz, loc, m); err != nil {
		t.Fatal(err)
	}
	gzf.Close()

	older := time.Date(2025, 2, 3, 4, 5, 6, 0, loc).Unix()
	if got := dpkgInstallTime(m, "curl", "amd64"); got != older {
		t.Fatalf("curl: got %d, want earliest %d", got, older)
	}
	if got := dpkgInstallTime(m, "curl", ""); got != older {
		t.Fatalf("curl without arch: got %d", got)
	}
	if got := dpkgInstallTime(m, "adduser", "all"); got != time.Date(2025, 2, 3, 4, 5, 7, 0, loc).Unix() {
		t.Fatalf("adduser logged without arch: got %d", got)
	}
	if got := dpkgInstallTime(m, "missing", "amd64"); got != 0 {
		t.Fatalf("missing: got %d", got)
	}
}
