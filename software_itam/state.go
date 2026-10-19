package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// retention is how long an item stays in the state after it was last seen.
// It's longer than a year so an app removed and reinstalled within an audit
// period keeps its first_seen date.
const retention = 400 * 24 * 60 * 60

// SeenEntry records when an item was first and last seen on this host.
type SeenEntry struct {
	FirstSeen int64 `json:"first_seen"`
	LastSeen  int64 `json:"last_seen"`
}

// CodesignEntry caches one codesign result. Mtime is the Info.plist modified
// time, which changes when an update replaces the bundle.
type CodesignEntry struct {
	Mtime     int64  `json:"mtime"`
	Authority string `json:"authority"`
	LastUsed  int64  `json:"last_used"`
}

// State is the extension's persisted memory.
type State struct {
	Items    map[string]*SeenEntry     `json:"items"`
	Codesign map[string]*CodesignEntry `json:"codesign"`
}

func newState() *State {
	return &State{Items: map[string]*SeenEntry{}, Codesign: map[string]*CodesignEntry{}}
}

// loadState reads the state file. A missing file is normal on the first run. A
// corrupt one returns an empty state and an error, so rows still come back.
func loadState(path string) (*State, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return newState(), nil
	}
	if err != nil {
		return newState(), err
	}
	st := newState()
	if err := json.Unmarshal(data, st); err != nil {
		return newState(), fmt.Errorf("corrupt state, starting over: %w", err)
	}
	if st.Items == nil {
		st.Items = map[string]*SeenEntry{}
	}
	if st.Codesign == nil {
		st.Codesign = map[string]*CodesignEntry{}
	}
	return st, nil
}

// saveState writes to a temp file and renames it, so a crash mid-write never
// leaves a half-written state that would reset every first_seen date.
func saveState(path string, st *State) error {
	data, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".software_itam_state-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Touch marks an item as seen now and returns its first_seen date.
func (s *State) Touch(key string, now int64) int64 {
	e, ok := s.Items[key]
	if !ok {
		e = &SeenEntry{FirstSeen: now}
		s.Items[key] = e
	}
	e.LastSeen = now
	return e.FirstSeen
}

// Prune drops items and cache entries not seen within the retention window.
func (s *State) Prune(now int64) {
	for k, e := range s.Items {
		if now-e.LastSeen > retention {
			delete(s.Items, k)
		}
	}
	for k, e := range s.Codesign {
		if now-e.LastUsed > retention {
			delete(s.Codesign, k)
		}
	}
}
