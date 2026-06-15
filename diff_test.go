package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// withStubDiff swaps the package-level ghDiff with a stub for the duration of a
// test so the diff plumbing stays hermetic (no real `gh` calls).
func withStubDiff(t *testing.T, fn func(number int) (string, error)) {
	t.Helper()
	orig := ghDiff
	ghDiff = fn
	t.Cleanup(func() { ghDiff = orig })
}

// drainBatch runs a (possibly batched) command and returns the flat list of
// concrete messages it produces. tea.Batch returns a tea.BatchMsg (a slice of
// commands) when executed, so this expands one level of batching to recover the
// individual messages (e.g. the body fetch and diff prefetch dispatched on
// detail entry). nil sub-commands are skipped.
func drainBatch(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	var out []tea.Msg
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			out = append(out, drainBatch(c)...)
		}
	default:
		out = append(out, msg)
	}
	return out
}

// openPRDetail opens the detail view on a single PR and returns the model.
func openPRDetail(t *testing.T) tea.Model {
	t.Helper()
	m := newModel()
	m.prList.SetItems([]list.Item{
		item{number: 7, title: "a pr", body: "pr body", type_: "pr"},
	})
	m.activeTab = tabPRs
	m.loading = false

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !tm.(model).detailOpen {
		t.Fatal("detail did not open")
	}
	return tm
}

// Pressing d on a PR detail toggles into the diff view, dispatches a fetch, and
// once the diffMsg arrives the diff is shown and cached.
func TestDiffToggleFetchesAndShows(t *testing.T) {
	calls := 0
	withStubDiff(t, func(number int) (string, error) {
		calls++
		if number != 7 {
			t.Errorf("fetchDiff called with number %d, want 7", number)
		}
		return "diff --git a/f b/f\n+added line\n-removed line\n", nil
	})

	tm := openPRDetail(t)

	// Toggle to diff: loading state on, fetch command dispatched.
	tm, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	mm := tm.(model)
	if !mm.detailShowDiff {
		t.Fatal("d did not toggle into diff view")
	}
	if !mm.detailDiffLoading {
		t.Error("expected detailDiffLoading=true while fetching")
	}
	if cmd == nil {
		t.Fatal("d toggle returned nil cmd; no diff fetch dispatched")
	}

	// Run the command to get the diffMsg, then deliver it.
	msg := cmd()
	dm, ok := msg.(diffMsg)
	if !ok {
		t.Fatalf("expected diffMsg, got %T", msg)
	}
	tm, _ = tm.Update(dm)
	mm = tm.(model)
	if mm.detailDiffLoading {
		t.Error("detailDiffLoading should be cleared after diffMsg")
	}
	if !strings.Contains(mm.detailDiff, "added line") {
		t.Errorf("diff not stored: %q", mm.detailDiff)
	}
	if mm.diffCache[cacheKey(mm.detailItem)] == "" {
		t.Error("diff not cached")
	}
	if calls != 1 {
		t.Errorf("fetchDiff called %d times, want 1", calls)
	}
}

// Toggling diff off then on again reuses the cache without re-fetching.
func TestDiffToggleUsesCache(t *testing.T) {
	calls := 0
	withStubDiff(t, func(number int) (string, error) {
		calls++
		return "cached diff body\n", nil
	})

	tm := openPRDetail(t)

	// First toggle: fetch.
	tm, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	tm, _ = tm.Update(cmd())

	// Toggle back to body.
	tm, cmd = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if tm.(model).detailShowDiff {
		t.Fatal("second d should toggle back to body")
	}
	if cmd != nil {
		t.Error("toggling back to body should not dispatch a fetch")
	}

	// Toggle to diff again: served from cache, no new fetch, no loading state.
	tm, cmd = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	mm := tm.(model)
	if !mm.detailShowDiff {
		t.Fatal("third d should show diff again")
	}
	if mm.detailDiffLoading {
		t.Error("cached diff should not show a loading state")
	}
	if cmd != nil {
		t.Error("cached diff toggle should not dispatch a fetch")
	}
	if calls != 1 {
		t.Errorf("fetchDiff called %d times, want 1 (cache reuse)", calls)
	}
}

// Opening a PR detail prefetches the diff in the background (no detailDiffLoading
// flag, no diff sub-view) so it is cached by the time the user toggles. The first
// toggle then serves from cache: no loading state and no second fetch.
func TestDiffPrefetchedOnDetailEntry(t *testing.T) {
	calls := 0
	withStubDiff(t, func(number int) (string, error) {
		calls++
		if number != 7 {
			t.Errorf("fetchDiff called with number %d, want 7", number)
		}
		return "diff --git a/f b/f\n+prefetched\n", nil
	})

	m := newModel()
	m.prList.SetItems([]list.Item{
		item{number: 7, title: "a pr", body: "pr body", type_: "pr"},
	})
	m.activeTab = tabPRs
	m.loading = false

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Enter the detail: the background diff prefetch is part of the returned cmd.
	tm, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := tm.(model)
	if mm.detailShowDiff {
		t.Error("entry must not switch into the diff sub-view")
	}
	if mm.detailDiffLoading {
		t.Error("background prefetch must not raise detailDiffLoading on entry")
	}
	if cmd == nil {
		t.Fatal("entry returned nil cmd; no diff prefetch dispatched")
	}

	// Run the batched entry cmd and deliver every resulting message; the diffMsg
	// caches the diff without opening the sub-view.
	for _, msg := range drainBatch(cmd) {
		tm, _ = tm.Update(msg)
	}
	mm = tm.(model)
	if mm.diffCache[cacheKey(mm.detailItem)] == "" {
		t.Fatal("diff was not prefetched into the cache on entry")
	}
	if mm.detailShowDiff {
		t.Error("prefetch delivery must not switch into the diff sub-view")
	}
	if calls != 1 {
		t.Fatalf("prefetch fetched %d times, want 1", calls)
	}

	// First toggle now serves from cache: no loading, no second fetch.
	tm, cmd = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	mm = tm.(model)
	if !mm.detailShowDiff {
		t.Fatal("d did not toggle into diff view")
	}
	if mm.detailDiffLoading {
		t.Error("toggle after prefetch should not show a loading state")
	}
	if cmd != nil {
		t.Error("toggle after prefetch should not dispatch a fetch")
	}
	if calls != 1 {
		t.Errorf("fetchDiff called %d times, want 1 (served from prefetch)", calls)
	}
	if !strings.Contains(mm.detailDiff, "prefetched") {
		t.Errorf("toggled view did not show the prefetched diff: %q", mm.detailDiff)
	}
}

// While the diff is loading (first toggle, prefetch not yet landed) the sub-view
// is left blank rather than showing an in-view "Loading diff..." placeholder; the
// activity is surfaced only in the status bar.
func TestDiffLoadingLeavesViewBlank(t *testing.T) {
	withStubDiff(t, func(number int) (string, error) { return "x\n", nil })

	tm := openPRDetail(t)
	// Toggle into the diff before delivering any diffMsg: loading state on.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	mm := tm.(model)
	if !mm.detailDiffLoading {
		t.Fatal("setup: expected detailDiffLoading while fetching")
	}
	// The view must be left genuinely blank while loading — not merely free of the
	// literal "Loading diff" string. Asserting empty catches any placeholder text.
	if got := mm.detailDiffContent(); got != "" {
		t.Errorf("loading diff should leave the view blank, got %q", got)
	}
	if !strings.Contains(mm.renderStatusBar(), loadingDiffIndicator) {
		t.Errorf("status bar should show the diff loading label, got %q", mm.renderStatusBar())
	}
}

// Pressing d on an issue detail is a no-op: no diff view, no fetch.
func TestDiffToggleNoOpForIssue(t *testing.T) {
	fetched := false
	withStubDiff(t, func(number int) (string, error) {
		fetched = true
		return "", nil
	})

	m := newModel()
	m.issueList.SetItems([]list.Item{item{number: 1, title: "iss", body: "b", type_: "issue"}})
	m.loading = false
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})

	tm, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	mm := tm.(model)
	if mm.detailShowDiff {
		t.Error("issue detail must not enter diff view")
	}
	if cmd != nil {
		t.Error("d on an issue should not dispatch a command")
	}
	if fetched {
		t.Error("d on an issue should not fetch a diff")
	}
}

// Closing the detail view resets the diff toggle so the next open starts on the
// body.
func TestDiffToggleResetOnClose(t *testing.T) {
	withStubDiff(t, func(number int) (string, error) { return "x\n", nil })

	tm := openPRDetail(t)
	tm, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	tm, _ = tm.Update(cmd())
	if !tm.(model).detailShowDiff {
		t.Fatal("setup: expected diff view")
	}

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if tm.(model).detailShowDiff {
		t.Error("diff toggle should reset when the detail view closes")
	}
}

// A failed diff fetch clears loading and surfaces the error in the view rather
// than blanking it.
func TestDiffFetchErrorShown(t *testing.T) {
	withStubDiff(t, func(number int) (string, error) { return "", errFake{} })

	tm := openPRDetail(t)
	tm, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	tm, _ = tm.Update(cmd())
	mm := tm.(model)
	if mm.detailDiffLoading {
		t.Error("detailDiffLoading should be cleared after a failed fetch")
	}
	if mm.detailDiffErr == nil {
		t.Error("expected detailDiffErr to be set after a failed fetch")
	}
	if !strings.Contains(mm.detailDiffContent(), "Error loading diff") {
		t.Errorf("expected diff error message in view, got %q", mm.detailDiffContent())
	}
}

// The PR help overlay advertises the d key, and the verb flips with state.
func TestHelpOverlayPRDiffHint(t *testing.T) {
	withStubDiff(t, func(number int) (string, error) { return "x\n", nil })

	tm := openPRDetail(t)
	help := tm.(model).renderHelp()
	if !strings.Contains(help, "show diff") {
		t.Errorf("PR help overlay missing diff hint: %q", help)
	}

	tm, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	tm, _ = tm.Update(cmd())
	help = tm.(model).renderHelp()
	if !strings.Contains(help, "show body") {
		t.Errorf("diff-view help overlay should offer 'show body': %q", help)
	}
}

// An issue help overlay must not advertise the diff key.
func TestHelpOverlayIssueNoDiffHint(t *testing.T) {
	m := newModel()
	m.issueList.SetItems([]list.Item{item{number: 1, title: "iss", body: "b", type_: "issue"}})
	m.loading = false
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})

	help := tm.(model).renderHelp()
	if strings.Contains(help, "diff") {
		t.Errorf("issue help overlay should not mention diff: %q", help)
	}
}

// Pressing r while the diff sub-view is open re-fetches the diff (not the body),
// and an updated diff replaces the cached one while preserving scroll position.
func TestDiffRefreshRefetchesDiff(t *testing.T) {
	version := "v1\n"
	withStubDiff(t, func(number int) (string, error) { return version, nil })

	tm := openPRDetail(t)
	tm, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	tm, _ = tm.Update(cmd())
	if !strings.Contains(tm.(model).detailDiff, "v1") {
		t.Fatalf("setup: expected v1 diff, got %q", tm.(model).detailDiff)
	}

	// r in the diff view must dispatch a diff fetch (not a body fetch).
	version = "v2\n"
	tm, cmd = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("r in diff view returned nil cmd")
	}
	msg := cmd()
	if _, ok := msg.(diffMsg); !ok {
		t.Fatalf("r in diff view dispatched %T, want diffMsg", msg)
	}
	tm, _ = tm.Update(msg)
	mm := tm.(model)
	if !strings.Contains(mm.detailDiff, "v2") {
		t.Errorf("diff not refreshed: %q", mm.detailDiff)
	}
	if mm.diffCache[cacheKey(mm.detailItem)] != "v2\n" {
		t.Errorf("diff cache not updated on refresh: %q", mm.diffCache[cacheKey(mm.detailItem)])
	}
}

// colorizeDiff wraps +/- lines in color escapes and leaves the underlying text
// intact; empty input yields empty output.
func TestColorizeDiff(t *testing.T) {
	if got := colorizeDiff(""); got != "" {
		t.Errorf("colorizeDiff(\"\") = %q, want empty", got)
	}
	in := "@@ -1 +1 @@\n+new\n-old\n unchanged"
	out := colorizeDiff(in)
	for _, want := range []string{"new", "old", "unchanged"} {
		if !strings.Contains(out, want) {
			t.Errorf("colorizeDiff dropped %q from output: %q", want, out)
		}
	}
}
