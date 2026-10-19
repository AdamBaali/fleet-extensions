package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStatePersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), stateFileName)

	st, err := loadState(path)
	if err != nil {
		t.Fatalf("missing file should not be an error: %v", err)
	}
	if got := st.Touch("apps|com.a|/Applications/A.app", 1000); got != 1000 {
		t.Fatalf("first touch = %d", got)
	}
	st.Codesign["/Applications/A.app"] = &CodesignEntry{Mtime: 5, Authority: "Software Signing", LastUsed: 1000}
	if err := saveState(path, st); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("state file mode %v, want 0600", fi.Mode().Perm())
	}

	st2, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := st2.Touch("apps|com.a|/Applications/A.app", 2000); got != 1000 {
		t.Fatalf("first_seen after reload = %d, want 1000", got)
	}
	if st2.Items["apps|com.a|/Applications/A.app"].LastSeen != 2000 {
		t.Fatal("last_seen not updated")
	}
	if st2.Codesign["/Applications/A.app"].Authority != "Software Signing" {
		t.Fatal("codesign cache not persisted")
	}
	leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".software_itam_state-*"))
	if len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestStatePrune(t *testing.T) {
	st := newState()
	st.Touch("old", 0)
	st.Touch("recent", retention)
	st.Codesign["old"] = &CodesignEntry{LastUsed: 0}
	st.Codesign["recent"] = &CodesignEntry{LastUsed: retention}
	st.Prune(retention + 10)
	if _, ok := st.Items["old"]; ok {
		t.Error("old item not pruned")
	}
	if _, ok := st.Items["recent"]; !ok {
		t.Error("recent item pruned")
	}
	if _, ok := st.Codesign["old"]; ok {
		t.Error("old codesign entry not pruned")
	}
	if _, ok := st.Codesign["recent"]; !ok {
		t.Error("recent codesign entry pruned")
	}
}

func TestStateCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), stateFileName)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := loadState(path)
	if err == nil {
		t.Fatal("corrupt state should return an error")
	}
	if st == nil || st.Items == nil || st.Codesign == nil {
		t.Fatal("corrupt state should still return a usable empty state")
	}
	if got := st.Touch("x", 42); got != 42 {
		t.Fatalf("touch on recovered state = %d", got)
	}
	if err := saveState(path, st); err != nil {
		t.Fatalf("saving over a corrupt file: %v", err)
	}
	if _, err := loadState(path); err != nil {
		t.Fatalf("state still corrupt after save: %v", err)
	}
}

func TestStateNullMaps(t *testing.T) {
	path := filepath.Join(t.TempDir(), stateFileName)
	if err := os.WriteFile(path, []byte(`{"items":null,"codesign":null}`), 0o600); err != nil {
		t.Fatal(err)
	}
	st, err := loadState(path)
	if err != nil {
		t.Fatal(err)
	}
	st.Touch("x", 1)
	st.Codesign["y"] = &CodesignEntry{}
}
