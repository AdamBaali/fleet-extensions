//go:build linux

package main

import (
	"context"
	"os"
	"testing"
)

// TestCollectLinuxLive runs the real collector on the test machine. It only
// asserts what any dpkg or rpm host guarantees, so it passes in CI and in a
// container alike.
func TestCollectLinuxLive(t *testing.T) {
	_, debErr := os.Stat(dpkgStatusPath)
	items := collect(context.Background(), newState(), 0)
	if debErr != nil && len(items) == 0 {
		t.Skip("no dpkg or rpm database on this machine")
	}
	if len(items) == 0 {
		t.Fatal("dpkg status exists but no items were collected")
	}
	withDate := 0
	for _, it := range items {
		if it.Name == "" || it.Version == "" {
			t.Errorf("item without name or version: %+v", it)
		}
		if d, _ := pickInstallDate(it); d > 0 {
			withDate++
		}
	}
	t.Logf("%d items, %d with an install date", len(items), withDate)
	if withDate == 0 {
		t.Error("no item has an install date")
	}
}
