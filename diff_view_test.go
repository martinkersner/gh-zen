package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// openDiffView opens a PR detail, toggles into the diff view, and delivers a
// multi-file diff so the parsed structure is populated.
func openDiffView(t *testing.T, width, height int) tea.Model {
	t.Helper()
	withStubDiff(t, func(number int) (string, error) { return sampleDiff, nil })

	m := newModel()
	m.prList.SetItems([]list.Item{
		item{number: 7, title: "a pr", body: "pr body", type_: "pr"},
	})
	m.activeTab = tabPRs
	m.loading = false

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: width, Height: height})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tm, cmd := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if cmd == nil {
		t.Fatal("d toggle dispatched no fetch")
	}
	tm, _ = tm.Update(cmd())
	mm := tm.(model)
	if !mm.detailShowDiff {
		t.Fatal("not in diff view")
	}
	if len(mm.detailFiles) != 4 {
		t.Fatalf("expected 4 parsed files, got %d", len(mm.detailFiles))
	}
	return tm
}

func press(tm tea.Model, s string) tea.Model {
	out, _ := tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	return out
}

func TestDiffSplitToggle(t *testing.T) {
	tm := openDiffView(t, 120, 40)
	if tm.(model).detailSplitView {
		t.Fatal("split should default off")
	}
	tm = press(tm, "s")
	mm := tm.(model)
	if !mm.detailSplitView {
		t.Fatal("s did not enable split view")
	}
	// Side-by-side content has the column separator.
	if !strings.Contains(mm.View(), "│") {
		t.Errorf("split view missing column separator in render")
	}
	tm = press(tm, "s")
	if tm.(model).detailSplitView {
		t.Error("second s did not toggle split off")
	}
}

func TestDiffSplitNarrowFallback(t *testing.T) {
	// Narrow terminal: split toggles state but renders unified (no separator).
	tm := openDiffView(t, 30, 40)
	tm = press(tm, "s")
	mm := tm.(model)
	if !mm.detailSplitView {
		t.Fatal("s should still flip the toggle even when narrow")
	}
	content := mm.detailDiffContent()
	if strings.Contains(content, "│") {
		t.Errorf("narrow split should fall back to unified (no separator): %q", content)
	}
}

func TestDiffFileNavigation(t *testing.T) {
	tm := openDiffView(t, 120, 40)
	mm := tm.(model)
	if mm.detailActiveFile != 0 {
		t.Fatalf("active file = %d, want 0 initially", mm.detailActiveFile)
	}
	if len(mm.detailFileOffsets) != 4 {
		t.Fatalf("file offsets = %d, want 4", len(mm.detailFileOffsets))
	}

	// ] advances and scrolls to the file's header offset.
	tm = press(tm, "]")
	mm = tm.(model)
	if mm.detailActiveFile != 1 {
		t.Errorf("after ] active file = %d, want 1", mm.detailActiveFile)
	}

	// ] clamps at the last file.
	tm = press(tm, "]")
	tm = press(tm, "]")
	tm = press(tm, "]")
	mm = tm.(model)
	if mm.detailActiveFile != 3 {
		t.Errorf("active file clamped to %d, want 3", mm.detailActiveFile)
	}

	// [ goes back and clamps at 0.
	tm = press(tm, "[")
	if tm.(model).detailActiveFile != 2 {
		t.Errorf("after [ active file = %d, want 2", tm.(model).detailActiveFile)
	}
	tm = press(tm, "[")
	tm = press(tm, "[")
	tm = press(tm, "[")
	if tm.(model).detailActiveFile != 0 {
		t.Errorf("active file clamped to %d, want 0", tm.(model).detailActiveFile)
	}
}

// With a viewport shorter than the diff, ] scrolls to a later file's header
// offset and [ scrolls back — i.e. navigation moves the visible window, not just
// the active-file cursor.
func TestDiffFileNavigationScrolls(t *testing.T) {
	// Short height so the rendered diff exceeds the viewport and offsets are
	// reachable (maxOffset > 0).
	tm := openDiffView(t, 120, 10)
	mm := tm.(model)
	if mm.detailViewport.YOffset != 0 {
		t.Fatalf("initial YOffset = %d, want 0", mm.detailViewport.YOffset)
	}
	lastOffset := mm.detailFileOffsets[len(mm.detailFileOffsets)-1]
	maxOffset := mm.detailViewport.TotalLineCount() - mm.detailViewport.Height
	if maxOffset <= 0 || lastOffset <= 0 {
		t.Skipf("diff not tall enough to scroll (max=%d last=%d)", maxOffset, lastOffset)
	}

	// Jump to the last file: offset should move downward.
	for i := 0; i < len(mm.detailFileOffsets); i++ {
		tm = press(tm, "]")
	}
	if got := tm.(model).detailViewport.YOffset; got == 0 {
		t.Errorf("] did not scroll viewport (YOffset still 0)")
	}

	// Jump back to the first file: offset should return toward the top.
	for i := 0; i < len(mm.detailFileOffsets); i++ {
		tm = press(tm, "[")
	}
	if got := tm.(model).detailViewport.YOffset; got != 0 {
		t.Errorf("[ back to first file YOffset = %d, want 0", got)
	}
}

func TestDiffOverviewToggle(t *testing.T) {
	tm := openDiffView(t, 120, 40)
	tm = press(tm, "f")
	mm := tm.(model)
	if !mm.detailShowOverview {
		t.Fatal("f did not show overview")
	}
	if !strings.Contains(mm.View(), "files changed") {
		t.Errorf("overview render missing summary: %q", mm.View())
	}
	tm = press(tm, "f")
	if tm.(model).detailShowOverview {
		t.Error("second f did not hide overview")
	}
}

func TestDiffNavKeysNoOpOutsideDiff(t *testing.T) {
	// On a PR body (not diff view) s/f/]/[ must not flip diff state.
	withStubDiff(t, func(number int) (string, error) { return sampleDiff, nil })
	tm := openPRDetail(t)
	for _, k := range []string{"s", "f", "]", "["} {
		tm = press(tm, k)
	}
	mm := tm.(model)
	if mm.detailShowDiff || mm.detailSplitView || mm.detailShowOverview {
		t.Errorf("diff keys mutated state outside diff view: %+v", mm)
	}
}

func TestDiffPresentationResetOnClose(t *testing.T) {
	tm := openDiffView(t, 120, 40)
	tm = press(tm, "s")
	tm = press(tm, "f")
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEsc})
	mm := tm.(model)
	if mm.detailSplitView || mm.detailShowOverview {
		t.Errorf("split/overview not reset on close: split=%v overview=%v", mm.detailSplitView, mm.detailShowOverview)
	}
}

func TestHelpOverlayDiffViewKeys(t *testing.T) {
	tm := openDiffView(t, 120, 40)
	help := tm.(model).renderHelp()
	for _, want := range []string{"split view", "next/prev file", "files overview"} {
		if !strings.Contains(help, want) {
			t.Errorf("diff-view help missing %q: %q", want, help)
		}
	}
}

// Resizing the terminal while the diff sub-view is open must re-run the diff-view
// branch of resizeDetailViewport: the viewport is re-dimensioned AND the
// per-file header offsets are recomputed for the new width (diff layout, and thus
// offsets, are width-dependent). Without that branch the offsets go stale and
// file navigation would scroll to wrong rows after a resize. (detail.go diff
// branch in resizeDetailViewport.)
func TestResizeInDiffViewRecomputesOffsets(t *testing.T) {
	tm := openDiffView(t, 120, 40)
	mm := tm.(model)
	if !mm.detailShowDiff {
		t.Fatal("setup: not in diff view")
	}
	wideOffsets := append([]int(nil), mm.detailFileOffsets...)
	if len(wideOffsets) != len(mm.detailFiles) {
		t.Fatalf("setup: offsets=%d files=%d", len(wideOffsets), len(mm.detailFiles))
	}

	// Resize narrower: the unified layout wraps long lines differently, so the
	// per-file offsets shift. The viewport width must follow the new terminal too.
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 50, Height: 20})
	mm = tm.(model)

	if len(mm.detailFileOffsets) != len(mm.detailFiles) {
		t.Errorf("offsets not recomputed after resize: got %d, want %d", len(mm.detailFileOffsets), len(mm.detailFiles))
	}
	wantW, _ := detailViewportSize(50, 20, 1)
	if mm.detailViewport.Width != wantW {
		t.Errorf("viewport width not resized: got %d, want %d", mm.detailViewport.Width, wantW)
	}
	// File navigation must still land on a valid offset (no stale index past the
	// re-rendered content); jumping to the last file should not panic and should
	// move the active-file cursor.
	tm = press(tm, "]")
	tm = press(tm, "]")
	tm = press(tm, "]")
	if got := tm.(model).detailActiveFile; got != len(mm.detailFiles)-1 {
		t.Errorf("after resize, ] navigation active file = %d, want %d", got, len(mm.detailFiles)-1)
	}
}
