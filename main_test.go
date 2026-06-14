package main

import (
	"os"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// TestMain pins lipgloss's global color profile to Ascii for the whole test
// package so rendered output is hermetic regardless of whether the test runner's
// stdout is a TTY, a pipe, or CI.
//
// With color enabled (TTY / CLICOLOR_FORCE), lipgloss wraps filter-match
// highlights in ANSI escapes, splitting otherwise-contiguous row bytes (e.g.
// "second issue beta" -> "second issue \x1b[...mbeta\x1b[0m"). That broke the
// bytes.Contains-based WaitFor matchers in the filter benchmark/e2e tests when
// run interactively. Pinning Ascii emits no ANSI, keeping the bytes contiguous.
//
// Tests that need color (number_color_test.go, statusbar_test.go) explicitly set
// their own profile and restore the previous one, so they are unaffected.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)
	os.Exit(m.Run())
}
