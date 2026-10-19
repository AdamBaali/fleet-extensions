package main

import (
	"strconv"
	"testing"
)

func TestPickInstallDateOrder(t *testing.T) {
	cases := []struct {
		name    string
		it      Item
		want    int64
		wantSrc string
	}{
		{"apps prefers install history", Item{Source: "apps", Candidates: []Candidate{{"bundle_created", 1}, {"spotlight_date_added", 2}, {"install_history", 3}}}, 3, "install_history"},
		{"apps falls back to spotlight", Item{Source: "apps", Candidates: []Candidate{{"install_history", 0}, {"spotlight_date_added", 2}, {"bundle_created", 1}}}, 2, "spotlight_date_added"},
		{"apps falls back to birth time", Item{Source: "apps", Candidates: []Candidate{{"install_history", 0}, {"spotlight_date_added", 0}, {"bundle_created", 1}}}, 1, "bundle_created"},
		{"programs prefers InstallDate", Item{Source: "programs", Candidates: []Candidate{{"registry_install_date", 5}, {"registry_key_written", 9}}}, 5, "registry_install_date"},
		{"programs falls back to key time", Item{Source: "programs", Candidates: []Candidate{{"registry_install_date", 0}, {"registry_key_written", 9}}}, 9, "registry_key_written"},
		{"deb prefers the log", Item{Source: "deb_packages", Candidates: []Candidate{{"dpkg_log", 4}, {"dpkg_list_changed", 8}}}, 4, "dpkg_log"},
		{"deb falls back to list time", Item{Source: "deb_packages", Candidates: []Candidate{{"dpkg_log", 0}, {"dpkg_list_changed", 8}}}, 8, "dpkg_list_changed"},
		{"nothing known", Item{Source: "apps"}, 0, ""},
		{"unknown source keeps its order", Item{Source: "other", Candidates: []Candidate{{"a", 0}, {"b", 7}}}, 7, "b"},
	}
	for _, c := range cases {
		got, src := pickInstallDate(c.it)
		if got != c.want || src != c.wantSrc {
			t.Errorf("%s: got %d %q, want %d %q", c.name, got, src, c.want, c.wantSrc)
		}
	}
}

func TestFormatCandidatesSkipsUnknown(t *testing.T) {
	got := formatCandidates([]Candidate{{"install_history", 0}, {"spotlight_date_added", 2}, {"bundle_created", 1}})
	if got != "spotlight_date_added=2,bundle_created=1" {
		t.Fatalf("got %q", got)
	}
}

func TestBuildRowsHasEveryColumn(t *testing.T) {
	items := []Item{
		{Source: "apps", Name: "A", Path: "/Applications/A.app", Identifier: "com.a"},
		{Source: "deb_packages", Name: "curl", Identifier: "curl", Arch: "amd64", Candidates: []Candidate{{"dpkg_log", 10}}},
	}
	st := newState()
	rows := buildRows(items, st, 100)
	if len(rows) != 2 {
		t.Fatalf("got %d rows", len(rows))
	}
	for i, row := range rows {
		if len(row) != len(Columns()) {
			t.Errorf("row %d has %d keys, want %d", i, len(row), len(Columns()))
		}
		for _, col := range Columns() {
			if _, ok := row[col.Name]; !ok {
				t.Errorf("row %d is missing column %s", i, col.Name)
			}
		}
		if row["first_seen"] != "100" {
			t.Errorf("row %d first_seen = %s", i, row["first_seen"])
		}
		if _, err := strconv.ParseInt(row["install_date"], 10, 64); err != nil {
			t.Errorf("row %d install_date not an integer: %q", i, row["install_date"])
		}
	}
	if rows[1]["install_date"] != "10" || rows[1]["install_date_source"] != "dpkg_log" || rows[1]["install_date_candidates"] != "dpkg_log=10" {
		t.Errorf("deb row: %+v", rows[1])
	}

	// A later run keeps the first date.
	rows = buildRows(items, st, 500)
	if rows[0]["first_seen"] != "100" {
		t.Errorf("first_seen moved to %s", rows[0]["first_seen"])
	}
}
