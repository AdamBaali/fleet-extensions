//go:build !darwin && !windows && !linux

package main

import "context"

// collect returns nothing on platforms the table doesn't support.
func collect(_ context.Context, _ *State, _ int64) []Item { return nil }
