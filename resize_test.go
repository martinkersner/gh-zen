package main

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// A WindowSizeMsg while the detail view is open resizes the detail viewport to
// the dimensions detailViewportSize computes for the new terminal size
// (width minus chrome, height minus the measured header and status bar).
func TestResizeDetailViewportDimensions(t *testing.T) {
	m := openDetailWithBody(t, "some body text that fits on a line", 80, 24)

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	mm := tm.(model)

	wantW, wantH := detailViewportSize(100, 40, lipgloss.Height(mm.detailHeader()))
	if mm.detailViewport.Width != wantW {
		t.Errorf("viewport width = %d, want %d", mm.detailViewport.Width, wantW)
	}
	if mm.detailViewport.Height != wantH {
		t.Errorf("viewport height = %d, want %d", mm.detailViewport.Height, wantH)
	}
}

// While a detail-view search is active, resizing re-wraps the body and
// recomputes matches against the new wrapping: the multi-word query "alpha beta"
// lands contiguously on one wrapped line at a wide width (1 match) but gets split
// across two lines once the terminal narrows (0 matches). The active-match index
// is clamped back into range when the match count shrinks below it.
func TestResizeWhileSearchingRecomputesAndClamps(t *testing.T) {
	body := "alpha beta gamma delta epsilon zeta"
	m := openDetailWithBody(t, body, 80, 24)

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	tm = typeRunes(tm, "alpha beta")
	mm := tm.(model)
	if len(mm.detailMatches) != 1 {
		t.Fatalf("matches at width 80 = %d, want 1", len(mm.detailMatches))
	}

	// Force a non-zero active index so the post-resize clamp is observable.
	mm.detailActiveMatch = 1
	tm = mm

	// Narrow terminal: "alpha" and "beta" wrap onto separate lines, so the
	// contiguous query no longer matches.
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 10, Height: 24})
	mm = tm.(model)
	if len(mm.detailMatches) != 0 {
		t.Errorf("matches at width 10 = %d, want 0 (query split across wrapped lines)", len(mm.detailMatches))
	}
	if mm.detailActiveMatch != 0 {
		t.Errorf("active match = %d, want 0 (clamped after match count shrank)", mm.detailActiveMatch)
	}
}

// While searching, widening the terminal re-joins the query onto one wrapped
// line and the match reappears, confirming matches are recomputed against the
// re-wrapped body (not stale).
func TestResizeWhileSearchingRematchesOnWiden(t *testing.T) {
	body := "alpha beta gamma delta epsilon zeta"
	// Start narrow: query is split, zero matches.
	m := openDetailWithBody(t, body, 10, 24)

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	tm = typeRunes(tm, "alpha beta")
	if n := len(tm.(model).detailMatches); n != 0 {
		t.Fatalf("matches at width 10 = %d, want 0", n)
	}

	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if n := len(tm.(model).detailMatches); n != 1 {
		t.Errorf("matches after widening = %d, want 1 (recomputed against re-wrapped body)", n)
	}
}

// Resizing while NOT searching does not populate matches: recomputeDetailMatches
// is a no-op (leaves detailMatches empty and detailActiveMatch at 0) when no
// search is active, even though the same body contains the would-be query text.
func TestResizeNotSearchingLeavesMatchesEmpty(t *testing.T) {
	m := openDetailWithBody(t, "alpha beta gamma delta epsilon zeta", 80, 24)
	if m.detailSearching {
		t.Fatal("precondition: should not be searching")
	}

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	mm := tm.(model)

	if len(mm.detailMatches) != 0 {
		t.Errorf("detailMatches = %d, want 0 when not searching", len(mm.detailMatches))
	}
	if mm.detailActiveMatch != 0 {
		t.Errorf("detailActiveMatch = %d, want 0 when not searching", mm.detailActiveMatch)
	}
}
