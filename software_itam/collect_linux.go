//go:build linux

package main

import (
	"compress/gzip"
	"context"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	rpmBin         = "/usr/bin/rpm"
	dpkgStatusPath = "/var/lib/dpkg/status"
	dpkgInfoDir    = "/var/lib/dpkg/info"
	dpkgLogGlob    = "/var/log/dpkg.log*"
	rpmTimeout     = 60 * time.Second
)

func collect(ctx context.Context, _ *State, _ int64) []Item {
	var items []Item
	items = append(items, collectRPM(ctx)...)
	items = append(items, collectDeb()...)
	return items
}

func collectRPM(ctx context.Context) []Item {
	bin := rpmBin
	if _, err := os.Stat(bin); err != nil {
		found, err := exec.LookPath("rpm")
		if err != nil {
			return nil
		}
		bin = found
	}
	cctx, cancel := context.WithTimeout(ctx, rpmTimeout)
	defer cancel()
	out, err := exec.CommandContext(cctx, bin, "-qa", "--qf", rpmQueryFormat).Output()
	if err != nil {
		log.Printf("software_itam: rpm -qa: %v", err)
		return nil
	}
	return parseRPMOutput(string(out))
}

func collectDeb() []Item {
	f, err := os.Open(dpkgStatusPath)
	if err != nil {
		return nil
	}
	pkgs, err := parseDpkgStatus(f)
	f.Close()
	if err != nil {
		log.Printf("software_itam: parsing %s: %v", dpkgStatusPath, err)
	}

	logTimes := readDpkgLogs()
	items := make([]Item, 0, len(pkgs))
	for _, p := range pkgs {
		it := Item{
			Source:     "deb_packages",
			Name:       p.Name,
			Version:    p.Version,
			Identifier: p.Name,
			Arch:       p.Arch,
			Publisher:  p.Maintainer,
		}
		if p.Maintainer != "" {
			it.PublisherSource = "deb_maintainer"
		}
		it.Candidates = []Candidate{
			{"dpkg_log", dpkgInstallTime(logTimes, p.Name, p.Arch)},
			{"dpkg_list_changed", listChanged(p.Name, p.Arch)},
		}
		items = append(items, it)
	}
	return items
}

// readDpkgLogs parses every rotated dpkg log once per query. logrotate keeps
// about a year of them, so packages installed before that fall back to the
// file list time.
func readDpkgLogs() map[string]int64 {
	m := map[string]int64{}
	files, _ := filepath.Glob(dpkgLogGlob)
	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		var r io.Reader = f
		if strings.HasSuffix(path, ".gz") {
			gz, err := gzip.NewReader(f)
			if err != nil {
				f.Close()
				continue
			}
			r = gz
		}
		if err := parseDpkgLog(r, time.Local, m); err != nil {
			log.Printf("software_itam: reading %s: %v", path, err)
		}
		f.Close()
	}
	return m
}

// listChanged returns the modified time of the package's file list, which
// dpkg rewrites on install and upgrade.
func listChanged(name, arch string) int64 {
	for _, n := range []string{name + ":" + arch + ".list", name + ".list"} {
		if fi, err := os.Stat(filepath.Join(dpkgInfoDir, n)); err == nil {
			return fi.ModTime().Unix()
		}
	}
	return 0
}
