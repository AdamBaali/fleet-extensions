//go:build darwin

package main

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

// bundleGlobs are the folders osquery's apps table searches most often. A
// single level of subfolders catches vendor folders such as
// /Applications/Microsoft Office/*.app.
var bundleGlobs = []string{
	"/Applications/*.app",
	"/Applications/*/*.app",
	"/Applications/Utilities/*.app",
	"/System/Applications/*.app",
	"/System/Applications/Utilities/*.app",
	"/Users/*/Applications/*.app",
}

const (
	installHistoryPath = "/Library/Receipts/InstallHistory.plist"
	mdlsBin            = "/usr/bin/mdls"
	codesignBin        = "/usr/bin/codesign"
	mdlsChunk          = 200
	codesignWorkers    = 4
	codesignTimeout    = 15 * time.Second
	mdlsTimeout        = 60 * time.Second
)

type bundle struct {
	path      string
	info      InfoPlist
	plistTime int64
	birth     int64
}

func collect(ctx context.Context, st *State, now int64) []Item {
	bundles := findBundles()

	var idx map[string]int64
	if data, err := os.ReadFile(installHistoryPath); err == nil {
		entries, err := parseInstallHistory(data)
		if err != nil {
			log.Printf("software_itam: parsing %s: %v", installHistoryPath, err)
		}
		idx = historyIndex(entries)
	}

	paths := make([]string, len(bundles))
	for i, b := range bundles {
		paths[i] = b.path
	}
	added := dateAdded(ctx, paths)
	authorities := authoritiesFor(ctx, bundles, st, now)

	items := make([]Item, 0, len(bundles))
	for i, b := range bundles {
		name := appName(b.info, b.path)
		it := Item{
			Source:     "apps",
			Name:       name,
			Version:    appVersion(b.info),
			Identifier: b.info.BundleIdentifier,
			Path:       b.path,
		}
		it.Publisher, it.PublisherSource = normalizePublisher(authorities[i], b.info.NSHumanReadableCopyright)
		it.Candidates = []Candidate{
			{"install_history", matchInstallHistory(idx, name, b.info.BundleName, strings.TrimSuffix(filepath.Base(b.path), ".app"))},
			{"spotlight_date_added", added[i]},
			{"bundle_created", b.birth},
		}
		items = append(items, it)
	}
	return items
}

func findBundles() []bundle {
	seen := map[string]bool{}
	var out []bundle
	for _, g := range bundleGlobs {
		matches, _ := filepath.Glob(g)
		for _, p := range matches {
			// A bundle inside another bundle is part of that app, not an app of
			// its own. Fleet's query drops these the same way.
			if strings.Contains(filepath.Dir(p), ".app") || seen[p] {
				continue
			}
			seen[p] = true
			b := bundle{path: p}
			plistPath := filepath.Join(p, "Contents", "Info.plist")
			if data, err := os.ReadFile(plistPath); err == nil {
				if info, err := parseInfoPlist(data); err == nil {
					b.info = info
				}
				if fi, err := os.Stat(plistPath); err == nil {
					b.plistTime = fi.ModTime().Unix()
				}
			}
			if fi, err := os.Stat(p); err == nil {
				if sys, ok := fi.Sys().(*syscall.Stat_t); ok {
					b.birth = sys.Birthtimespec.Sec
				}
			}
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

// dateAdded asks Spotlight for kMDItemDateAdded in batches, because one mdls
// process per app would cost more than the rest of the query together.
func dateAdded(ctx context.Context, paths []string) []int64 {
	res := make([]int64, len(paths))
	for start := 0; start < len(paths); start += mdlsChunk {
		end := min(start+mdlsChunk, len(paths))
		cctx, cancel := context.WithTimeout(ctx, mdlsTimeout)
		args := append([]string{"-raw", "-name", "kMDItemDateAdded"}, paths[start:end]...)
		out, err := exec.CommandContext(cctx, mdlsBin, args...).Output()
		cancel()
		if err != nil && len(out) == 0 {
			log.Printf("software_itam: mdls: %v", err)
			continue
		}
		copy(res[start:end], parseMdlsOutput(out, end-start))
	}
	return res
}

// authoritiesFor returns the signing authority per bundle, from the cache when
// the bundle's Info.plist hasn't changed since the last codesign run.
func authoritiesFor(ctx context.Context, bundles []bundle, st *State, now int64) []string {
	res := make([]string, len(bundles))
	var todo []int
	for i, b := range bundles {
		if e, ok := st.Codesign[b.path]; ok && e.Mtime == b.plistTime {
			res[i] = e.Authority
			e.LastUsed = now
			continue
		}
		todo = append(todo, i)
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < codesignWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				cctx, cancel := context.WithTimeout(ctx, codesignTimeout)
				// codesign writes to stderr and exits non-zero for unsigned
				// code, so the output matters and the exit status doesn't.
				out, _ := exec.CommandContext(cctx, codesignBin, "-dvv", bundles[i].path).CombinedOutput()
				cancel()
				res[i] = parseCodesignAuthority(string(out))
			}
		}()
	}
	for _, i := range todo {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	for _, i := range todo {
		st.Codesign[bundles[i].path] = &CodesignEntry{Mtime: bundles[i].plistTime, Authority: res[i], LastUsed: now}
	}
	return res
}
