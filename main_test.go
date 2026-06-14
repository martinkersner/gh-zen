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
//
// TestMain also isolates the config dir for the whole package. newModel() calls
// applyPalette(loadPalette()), and loadPalette -> loadConfig -> configPath uses
// os.UserConfigDir(). Without isolation that resolves to the real user config,
// so a developer's persisted non-default palette (e.g. {"palette":"Dracula"})
// poisons the package-level color globals and breaks color-asserting tests
// (TestStatusBarUniformColor, TestPaletteDarkMatchesTokyoNight). Pointing both
// HOME (macOS Application Support) and XDG_CONFIG_HOME (Linux) at an empty temp
// dir makes loadPalette fall back to the default palette regardless of the host
// config. Issue #118.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.Ascii)

	dir, err := os.MkdirTemp("", "gh-zen-test-config")
	if err != nil {
		panic("TestMain: create temp config dir: " + err.Error())
	}
	os.Setenv("HOME", dir)
	os.Setenv("XDG_CONFIG_HOME", dir)

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
