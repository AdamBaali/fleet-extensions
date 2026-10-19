package main

import (
	"testing"
	"time"
)

func TestParseInstallDate(t *testing.T) {
	loc := time.FixedZone("test", 2*60*60)
	day := time.Date(2026, 10, 19, 0, 0, 0, 0, loc).Unix()
	cases := map[string]int64{
		"20261019":   day,
		"2026-10-19": day,
		"10/19/2026": day,
		"2026/10/19": day,
		" 20261019 ": day,
		"":           0,
		"not a date": 0,
		"19700101":   0,
		"21500101":   0,
	}
	for in, want := range cases {
		if got := parseInstallDate(in, loc); got != want {
			t.Errorf("parseInstallDate(%q) = %d, want %d", in, got, want)
		}
	}
}
