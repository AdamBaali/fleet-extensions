package main

import (
	"strconv"
	"strings"

	"github.com/osquery/osquery-go/plugin/table"
)

// Candidate is one install date source and the date it gave, in unix seconds.
type Candidate struct {
	Source string
	Unix   int64
}

// Item is one app or package before it becomes a row.
type Item struct {
	Source          string
	Name            string
	Version         string
	Identifier      string
	Path            string
	Arch            string
	Publisher       string
	PublisherSource string
	Candidates      []Candidate
}

// installDateOrder is the preference per software source. The first source
// with a date wins. macOS prefers the installer's own history because it keeps
// the first install across updates; Spotlight's date added and the bundle's
// birth time move when an update replaces the bundle.
var installDateOrder = map[string][]string{
	"apps":         {"install_history", "spotlight_date_added", "bundle_created"},
	"programs":     {"registry_install_date", "registry_key_written"},
	"rpm_packages": {"rpm_install_time"},
	"deb_packages": {"dpkg_log", "dpkg_list_changed"},
}

// Columns returns the columns for the software_itam table. Dates are BIGINT so
// they stay correct past 2038.
func Columns() []table.ColumnDefinition {
	return []table.ColumnDefinition{
		table.TextColumn("source"),
		table.TextColumn("name"),
		table.TextColumn("version"),
		table.TextColumn("identifier"),
		table.TextColumn("path"),
		table.TextColumn("arch"),
		table.TextColumn("publisher"),
		table.TextColumn("publisher_source"),
		table.BigIntColumn("install_date"),
		table.TextColumn("install_date_source"),
		table.TextColumn("install_date_candidates"),
		table.BigIntColumn("first_seen"),
	}
}

// pickInstallDate returns the first candidate with a date, in the order set for
// the item's source. Unknown sources fall back to the candidates' own order.
func pickInstallDate(it Item) (int64, string) {
	order, ok := installDateOrder[it.Source]
	if !ok {
		for _, c := range it.Candidates {
			order = append(order, c.Source)
		}
	}
	for _, src := range order {
		for _, c := range it.Candidates {
			if c.Source == src && c.Unix > 0 {
				return c.Unix, c.Source
			}
		}
	}
	return 0, ""
}

func formatCandidates(cs []Candidate) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		if c.Unix > 0 {
			parts = append(parts, c.Source+"="+strconv.FormatInt(c.Unix, 10))
		}
	}
	return strings.Join(parts, ",")
}

func stateKey(it Item) string {
	return it.Source + "|" + it.Identifier + "|" + it.Path
}

// buildRows turns items into rows and records each item in the state.
func buildRows(items []Item, st *State, now int64) []map[string]string {
	rows := make([]map[string]string, 0, len(items))
	for _, it := range items {
		date, src := pickInstallDate(it)
		first := st.Touch(stateKey(it), now)
		rows = append(rows, map[string]string{
			"source":                  it.Source,
			"name":                    it.Name,
			"version":                 it.Version,
			"identifier":              it.Identifier,
			"path":                    it.Path,
			"arch":                    it.Arch,
			"publisher":               it.Publisher,
			"publisher_source":        it.PublisherSource,
			"install_date":            strconv.FormatInt(date, 10),
			"install_date_source":     src,
			"install_date_candidates": formatCandidates(it.Candidates),
			"first_seen":              strconv.FormatInt(first, 10),
		})
	}
	return rows
}
