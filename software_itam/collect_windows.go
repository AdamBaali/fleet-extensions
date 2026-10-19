//go:build windows

package main

import (
	"context"
	"log"
	"strings"
	"time"

	"golang.org/x/sys/windows/registry"
)

const uninstallPath = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`

func collect(_ context.Context, _ *State, _ int64) []Item {
	var items []Item
	items = append(items, readUninstall(registry.LOCAL_MACHINE, uninstallPath)...)
	items = append(items, readUninstall(registry.LOCAL_MACHINE, `SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`)...)

	users, err := registry.USERS.ReadSubKeyNames(-1)
	if err != nil {
		log.Printf("software_itam: listing HKEY_USERS: %v", err)
	}
	for _, sid := range users {
		// The _Classes hives hold file associations, not installs.
		if strings.HasSuffix(sid, "_Classes") {
			continue
		}
		items = append(items, readUninstall(registry.USERS, sid+`\`+uninstallPath)...)
	}
	return items
}

func readUninstall(root registry.Key, path string) []Item {
	k, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS|registry.QUERY_VALUE)
	if err != nil {
		return nil
	}
	defer k.Close()
	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		log.Printf("software_itam: listing %s: %v", path, err)
		return nil
	}

	var items []Item
	for _, n := range names {
		sk, err := registry.OpenKey(k, n, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		name := str(sk, "DisplayName")
		// osquery's programs table skips keys without a display name, and the
		// exporter joins on name, so this keeps both tables in step.
		if name == "" {
			sk.Close()
			continue
		}
		it := Item{
			Source:     "programs",
			Name:       name,
			Version:    str(sk, "DisplayVersion"),
			Identifier: n,
			Path:       str(sk, "InstallLocation"),
			Publisher:  str(sk, "Publisher"),
		}
		if it.Publisher != "" {
			it.PublisherSource = "registry_publisher"
		}
		it.Candidates = append(it.Candidates, Candidate{"registry_install_date", parseInstallDate(str(sk, "InstallDate"), time.Local)})
		// The key's last write time is set when the installer creates it, so
		// it stands in when InstallDate is missing. A repair or update can
		// rewrite the key, which is why it's the fallback and not the default.
		if ki, err := sk.Stat(); err == nil {
			it.Candidates = append(it.Candidates, Candidate{"registry_key_written", ki.ModTime().Unix()})
		}
		sk.Close()
		items = append(items, it)
	}
	return items
}

func str(k registry.Key, name string) string {
	v, _, err := k.GetStringValue(name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}
