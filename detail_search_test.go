package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

// openDetailWithBody returns a model with a single issue's detail view open at
// the given terminal size, ready to receive key input.
func openDetailWithBody(t *testing.T, body string, w, h int) model {
	t.Helper()
	m := newModel()
	m.issueList.SetItems([]list.Item{
		item{number: 1, title: "searchable", body: body, type_: "issue"},
	})
	m.loading = false

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: h})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := tm.(model)
	if !mm.detailOpen {
		t.Fatal("detail did not open")
	}
	return mm
}

func typeRunes(tm tea.Model, s string) tea.Model {
	for _, r := range s {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return tm
}

// Pressing "/" in the detail view enters search mode; typing builds a query and
// finds matches.
func TestDetailSearchEnterAndType(t *testing.T) {
	body := "alpha beta gamma\nbeta delta beta"
	m := openDetailWithBody(t, body, 80, 24)

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !tm.(model).detailSearching {
		t.Fatal("'/' did not enter search mode")
	}

	tm = typeRunes(tm, "beta")
	mm := tm.(model)
	if mm.detailQuery != "beta" {
		t.Errorf("query = %q, want %q", mm.detailQuery, "beta")
	}
	// "beta" appears 3 times in the body.
	if len(mm.detailMatches) != 3 {
		t.Fatalf("matches = %d, want 3", len(mm.detailMatches))
	}
	if mm.detailActiveMatch != 0 {
		t.Errorf("active match = %d, want 0 on fresh query", mm.detailActiveMatch)
	}
}

// ctrl+n / ctrl+p step through matches and wrap around while in search mode.
func TestDetailSearchJumpWrap(t *testing.T) {
	body := "beta one\nbeta two\nbeta three"
	m := openDetailWithBody(t, body, 80, 24)

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	tm = typeRunes(tm, "beta")
	if n := len(tm.(model).detailMatches); n != 3 {
		t.Fatalf("matches = %d, want 3", n)
	}

	// Forward: 0 -> 1 -> 2 -> wrap 0.
	for _, want := range []int{1, 2, 0} {
		tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
		if got := tm.(model).detailActiveMatch; got != want {
			t.Fatalf("ctrl+n active = %d, want %d", got, want)
		}
	}
	// Backward from 0 wraps to 2.
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if got := tm.(model).detailActiveMatch; got != 2 {
		t.Fatalf("ctrl+p active = %d, want 2", got)
	}
}

// Jumping to a match far down a long body scrolls the viewport so the active
// match's line is within the visible window.
func TestDetailSearchScrollsToMatch(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 60; i++ {
		sb.WriteString("filler line\n")
	}
	sb.WriteString("needle here\n")
	m := openDetailWithBody(t, sb.String(), 40, 12)

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	tm = typeRunes(tm, "needle")
	mm := tm.(model)
	if len(mm.detailMatches) != 1 {
		t.Fatalf("matches = %d, want 1", len(mm.detailMatches))
	}
	matchLine := mm.detailMatches[0].line
	off := mm.detailViewport.YOffset
	vh := mm.detailViewport.Height
	if matchLine < off || matchLine >= off+vh {
		t.Errorf("active match line %d not visible in window [%d,%d)", matchLine, off, off+vh)
	}
}

// esc exits search mode and clears the query/matches; the detail view stays
// open.
func TestDetailSearchEscClears(t *testing.T) {
	m := openDetailWithBody(t, "alpha beta gamma", 80, 24)

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	tm = typeRunes(tm, "beta")
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEsc})

	mm := tm.(model)
	if mm.detailSearching {
		t.Error("esc did not exit search mode")
	}
	if mm.detailQuery != "" || len(mm.detailMatches) != 0 {
		t.Errorf("esc did not clear query/matches: q=%q matches=%d", mm.detailQuery, len(mm.detailMatches))
	}
	if !mm.detailOpen {
		t.Error("esc in search mode closed the detail view; should only exit search")
	}
}

// Backspace shrinks the query and re-finds matches.
func TestDetailSearchBackspace(t *testing.T) {
	m := openDetailWithBody(t, "beta betax", 80, 24)

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	tm = typeRunes(tm, "betax")
	if n := len(tm.(model).detailMatches); n != 1 {
		t.Fatalf("matches for 'betax' = %d, want 1", n)
	}
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	mm := tm.(model)
	if mm.detailQuery != "beta" {
		t.Fatalf("query after backspace = %q, want %q", mm.detailQuery, "beta")
	}
	if len(mm.detailMatches) != 2 {
		t.Errorf("matches for 'beta' = %d, want 2", len(mm.detailMatches))
	}
}

// When NOT in search mode, ctrl+n/ctrl+p keep their one-line scroll behavior.
func TestDetailCtrlNPScrollsWhenNotSearching(t *testing.T) {
	body := strings.Repeat("line of text\n", 100)
	m := openDetailWithBody(t, body, 40, 10)

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if off := tm.(model).detailViewport.YOffset; off != 1 {
		t.Errorf("ctrl+n (not searching): want offset 1, got %d", off)
	}
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if off := tm.(model).detailViewport.YOffset; off != 0 {
		t.Errorf("ctrl+p (not searching): want offset 0, got %d", off)
	}
}

// When the active match and another match land on the same wrapped line, each
// is highlighted with its own style (regression: a two-pass StyleRunes approach
// misaligned the second set of indices once the first inserted ANSI escapes).
func TestDetailSearchHighlightActiveAndOtherSameLine(t *testing.T) {
	m := openDetailWithBody(t, "beta and beta again", 80, 24)

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	tm = typeRunes(tm, "beta")
	mm := tm.(model)
	if len(mm.detailMatches) != 2 {
		t.Fatalf("matches = %d, want 2", len(mm.detailMatches))
	}
	// Active is match 0; both styled fragments must appear and the literal
	// "beta" text must survive intact (not corrupted by overlapping escapes).
	content := mm.detailBodyContent()
	if strings.Count(content, "beta") != 2 {
		t.Errorf("rendered content lost a match: %q", content)
	}
	activeSeq := detailActiveMatchStyle.Render("b")
	otherSeq := detailMatchStyle.Render("b")
	if !strings.Contains(content, activeSeq) {
		t.Errorf("active match style not present in rendered content")
	}
	if !strings.Contains(content, otherSeq) {
		t.Errorf("non-active match style not present in rendered content")
	}
}
