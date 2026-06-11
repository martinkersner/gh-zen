package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/exp/teatest"
)

// End-to-end tests drive the whole tea.Program against an in-memory terminal
// (teatest), exercising the bootstrap + full render loop (Init/Update/View)
// that unit tests bypass by calling Update directly.
//
// These flows are fully offline: the data-fetch cmd (fetchIssuesAndPRs, a
// package var) is swapped for a hermetic source that returns seeded items, so
// Init's initial load and the 5s tick refresh both produce the same fixed list
// without touching the network. Network-backed body/diff *fetches* still can't
// run hermetically here — that depends on the fake client from #43 — so the
// diff flow asserts only on the synchronous "Loading diff..." frame the toggle
// renders before the (stubbed) fetch resolves.

const (
	e2eTermWidth  = 80
	e2eTermHeight = 24
	// e2eWaitTimeout bounds each WaitFor so a missed condition fails fast
	// rather than hanging the suite. Generous vs. the in-memory render loop.
	e2eWaitTimeout = 3 * time.Second
	// e2eFinalTimeout bounds the final program shutdown after Quit.
	e2eFinalTimeout = 3 * time.Second
)

// seedData is the dataMsg used to populate the lists offline. Bodies are
// inlined so opening a detail view needs no network fetch (cachedBody falls
// back to item.body).
func seedData() dataMsg {
	return dataMsg{
		issues: []list.Item{
			item{number: 11, title: "first issue alpha", body: "issue body line one\nneedle marker here\nissue body line three", type_: "issue"},
			item{number: 12, title: "second issue beta", body: "another body", type_: "issue"},
		},
		prs: []list.Item{
			item{number: 21, title: "first pr gamma", body: "pr body text", type_: "pr"},
		},
	}
}

// stubFetch swaps the network-backed fetch cmds for hermetic ones for the
// duration of the test, restoring the originals on cleanup. This makes Init's
// load and the tick refresh return the fixed seed data, and the PR diff toggle
// return a fixed diff, so no test touches the network or races a live fetch.
func stubFetch(t *testing.T) {
	t.Helper()
	origFetch := fetchIssuesAndPRs
	origDiff := ghDiff
	fetchIssuesAndPRs = func() tea.Cmd {
		return func() tea.Msg { return seedData() }
	}
	ghDiff = func(int) (string, error) { return "+added line\n-removed line\n", nil }
	t.Cleanup(func() {
		fetchIssuesAndPRs = origFetch
		ghDiff = origDiff
	})
}

// newSeededModel returns a running teatest program whose list is populated by
// the stubbed fetch (no network).
//
// It deliberately does NOT consume the output: teatest's tm.Output() is a
// consuming reader, and each WaitFor only sees bytes written since the previous
// read. So the very first WaitFor in each test (which waits for the seeded
// list) must run against the un-drained reader; reading here would swallow the
// initial frames and leave later asserts blocked on an idle program.
func newSeededModel(t *testing.T) *teatest.TestModel {
	t.Helper()
	stubFetch(t)
	return teatest.NewTestModel(t, newModel(), teatest.WithInitialTermSize(e2eTermWidth, e2eTermHeight))
}

// waitForList waits for the seeded list to render. Used as the first wait in
// flows that start from the list view.
func waitForList(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("first issue alpha"))
	}, teatest.WithDuration(e2eWaitTimeout))
}

// quit tears down the program and waits for it to finish, bounded by a timeout
// so a stuck shutdown fails loudly instead of hanging. ctrl+c is used (not 'q')
// because it quits from every mode — in the detail/search views 'q' is consumed
// as a back/query key, not a quit.
func quit(t *testing.T, tm *teatest.TestModel) {
	t.Helper()
	tm.Send(tea.KeyMsg{Type: tea.KeyCtrlC})
	tm.WaitFinished(t, teatest.WithFinalTimeout(e2eFinalTimeout))
}

// The list renders the seeded issues with the tabs row above it.
func TestE2EListRender(t *testing.T) {
	tm := newSeededModel(t)
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Issues")) &&
			bytes.Contains(b, []byte("first issue alpha")) &&
			bytes.Contains(b, []byte("second issue beta"))
	}, teatest.WithDuration(e2eWaitTimeout))
	quit(t, tm)
}

// tab switches to the PRs tab and renders the seeded PR.
func TestE2ETabSwitch(t *testing.T) {
	tm := newSeededModel(t)
	waitForList(t, tm)
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("first pr gamma"))
	}, teatest.WithDuration(e2eWaitTimeout))
	quit(t, tm)
}

// '?' opens the keyboard-shortcuts overlay over the list.
func TestE2EHelpOverlay(t *testing.T) {
	tm := newSeededModel(t)
	waitForList(t, tm)
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("Keyboard shortcuts"))
	}, teatest.WithDuration(e2eWaitTimeout))
	// esc dismisses it, returning to the list.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("first issue alpha"))
	}, teatest.WithDuration(e2eWaitTimeout))
	quit(t, tm)
}

// enter opens the detail view for the selected issue; its body (seeded inline)
// renders without any network fetch.
func TestE2EOpenDetail(t *testing.T) {
	tm := newSeededModel(t)
	waitForList(t, tm)
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("#11 first issue alpha")) &&
			bytes.Contains(b, []byte("issue body line one"))
	}, teatest.WithDuration(e2eWaitTimeout))
	// esc returns to the list.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("second issue beta"))
	}, teatest.WithDuration(e2eWaitTimeout))
	quit(t, tm)
}

// Filter mode ('/') filters the list to matching rows; the live query shows in
// the status bar and non-matching rows drop out.
func TestE2EFilter(t *testing.T) {
	tm := newSeededModel(t)
	waitForList(t, tm)
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	tm.Type("beta")
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		// "second issue beta" matches; "first issue alpha" is filtered out.
		return bytes.Contains(b, []byte("second issue beta")) &&
			!bytes.Contains(b, []byte("first issue alpha"))
	}, teatest.WithDuration(e2eWaitTimeout))
	// esc clears the filter; the filtered-out row reappears.
	tm.Send(tea.KeyMsg{Type: tea.KeyEsc})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("first issue alpha"))
	}, teatest.WithDuration(e2eWaitTimeout))
	quit(t, tm)
}

// In an open detail view, '/' enters in-detail search; typing a query that
// occurs in the body surfaces the live match count in the status bar.
func TestE2EDetailSearch(t *testing.T) {
	tm := newSeededModel(t)
	waitForList(t, tm)
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("issue body line one"))
	}, teatest.WithDuration(e2eWaitTimeout))

	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	tm.Type("needle")
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		// status bar shows "search: needle (1/1)" for the single match.
		return bytes.Contains(b, []byte("search: needle")) &&
			bytes.Contains(b, []byte("(1/1)"))
	}, teatest.WithDuration(e2eWaitTimeout))
	quit(t, tm)
}

// 'd' on a PR detail toggles the diff sub-view, which fetches and renders the
// PR diff. The real diff fetch is network-backed (deferred to #43); here the
// fetch is stubbed (see stubFetch) so the colorized diff renders deterministically.
func TestE2EDetailDiffToggle(t *testing.T) {
	tm := newSeededModel(t)
	waitForList(t, tm)
	// Switch to PRs, open the PR detail.
	tm.Send(tea.KeyMsg{Type: tea.KeyTab})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("first pr gamma"))
	}, teatest.WithDuration(e2eWaitTimeout))
	tm.Send(tea.KeyMsg{Type: tea.KeyEnter})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("#21 first pr gamma"))
	}, teatest.WithDuration(e2eWaitTimeout))

	// Toggle the diff; the stubbed fetch resolves to the fixed diff body.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("added line")) &&
			bytes.Contains(b, []byte("removed line"))
	}, teatest.WithDuration(e2eWaitTimeout))
	// Toggle back to the body.
	tm.Send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("pr body text"))
	}, teatest.WithDuration(e2eWaitTimeout))
	quit(t, tm)
}

// A WindowSizeMsg reflows the layout: after narrowing the terminal the list
// still renders its rows (no panic, no blank frame), exercising the full
// resize path through the running program.
func TestE2EWindowResize(t *testing.T) {
	tm := newSeededModel(t)
	waitForList(t, tm)
	tm.Send(tea.WindowSizeMsg{Width: 40, Height: 12})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("first issue alpha"))
	}, teatest.WithDuration(e2eWaitTimeout))
	// Widen again; layout still renders.
	tm.Send(tea.WindowSizeMsg{Width: 120, Height: 40})
	teatest.WaitFor(t, tm.Output(), func(b []byte) bool {
		return bytes.Contains(b, []byte("second issue beta"))
	}, teatest.WithDuration(e2eWaitTimeout))
	quit(t, tm)
}
