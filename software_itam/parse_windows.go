package main

import (
	"strings"
	"time"
)

// installDateLayouts are the InstallDate formats seen in Uninstall keys. The
// documented one is YYYYMMDD; some installers write a localized date instead.
var installDateLayouts = []string{"20060102", "2006-01-02", "1/2/2006", "01/02/2006", "2006/01/02"}

// parseInstallDate returns the start of that day in loc, or 0 when the value
// is empty, unparseable, or outside a plausible range.
func parseInstallDate(s string, loc *time.Location) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	for _, layout := range installDateLayouts {
		t, err := time.ParseInLocation(layout, s, loc)
		if err != nil {
			continue
		}
		if t.Year() < 1990 || t.Year() > 2100 {
			return 0
		}
		return t.Unix()
	}
	return 0
}
