package main

import (
	"bufio"
	"io"
	"strconv"
	"strings"
	"time"
)

// rpmQueryFormat is passed to `rpm -qa --qf`. Tabs never appear in these tags.
const rpmQueryFormat = "%{NAME}\t%{VERSION}\t%{RELEASE}\t%{ARCH}\t%{VENDOR}\t%{INSTALLTIME}\n"

// parseRPMOutput turns `rpm -qa --qf rpmQueryFormat` output into items.
func parseRPMOutput(out string) []Item {
	var items []Item
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\t")
		if len(f) != 6 || f[0] == "" {
			continue
		}
		vendor := f[4]
		if vendor == "(none)" {
			vendor = ""
		}
		it := Item{Source: "rpm_packages", Name: f[0], Version: f[1], Identifier: f[0], Arch: f[3], Publisher: vendor}
		if vendor != "" {
			it.PublisherSource = "rpm_vendor"
		}
		if ts, err := strconv.ParseInt(strings.TrimSpace(f[5]), 10, 64); err == nil && ts > 0 {
			it.Candidates = append(it.Candidates, Candidate{"rpm_install_time", ts})
		}
		items = append(items, it)
	}
	return items
}

// DebPackage is one installed package from /var/lib/dpkg/status.
type DebPackage struct {
	Name, Version, Arch, Maintainer string
}

// parseDpkgStatus returns packages whose Status is "install ok installed",
// the same filter Fleet's software query uses.
func parseDpkgStatus(r io.Reader) ([]DebPackage, error) {
	var pkgs []DebPackage
	var cur DebPackage
	var status string
	flush := func() {
		if cur.Name != "" && status == "install ok installed" {
			pkgs = append(pkgs, cur)
		}
		cur, status = DebPackage{}, ""
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			flush()
			continue
		}
		if line[0] == ' ' || line[0] == '\t' {
			continue
		}
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		switch k {
		case "Package":
			cur.Name = v
		case "Status":
			status = v
		case "Version":
			cur.Version = v
		case "Architecture":
			cur.Arch = v
		case "Maintainer":
			cur.Maintainer = v
		}
	}
	flush()
	return pkgs, sc.Err()
}

const dpkgLogLayout = "2006-01-02 15:04:05"

// parseDpkgLog records the earliest "install" line per package into m. Keys
// are both "pkg:arch" and bare "pkg", because older logs omit the arch.
func parseDpkgLog(r io.Reader, loc *time.Location, m map[string]int64) error {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[2] != "install" {
			continue
		}
		t, err := time.ParseInLocation(dpkgLogLayout, f[0]+" "+f[1], loc)
		if err != nil {
			continue
		}
		u := t.Unix()
		keys := []string{f[3]}
		if name, _, ok := strings.Cut(f[3], ":"); ok {
			keys = append(keys, name)
		}
		for _, k := range keys {
			if cur, ok := m[k]; !ok || u < cur {
				m[k] = u
			}
		}
	}
	return sc.Err()
}

// dpkgInstallTime looks a package up in the parsed log, arch-specific first.
func dpkgInstallTime(m map[string]int64, name, arch string) int64 {
	if arch != "" {
		if u, ok := m[name+":"+arch]; ok {
			return u
		}
	}
	return m[name]
}
