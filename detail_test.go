package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestDetailViewportSize(t *testing.T) {
	cases := []struct {
		name         string
		w, h, hdr    int
		wantW, wantH int
	}{
		{"normal", 80, 24, detailHeaderHeight, 78, 22},
		{"tiny height", 80, 2, detailHeaderHeight, 78, 1},
		{"zero", 0, 0, detailHeaderHeight, 1, 1},
		{"negative", -5, -5, detailHeaderHeight, 1, 1},
		{"wrapped header reserves more rows", 80, 24, 3, 78, 20},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotW, gotH := detailViewportSize(c.w, c.h, c.hdr)
			if gotW != c.wantW || gotH != c.wantH {
				t.Errorf("detailViewportSize(%d,%d,%d) = (%d,%d), want (%d,%d)",
					c.w, c.h, c.hdr, gotW, gotH, c.wantW, c.wantH)
			}
		})
	}
}

// A long title on a narrow terminal wraps to multiple rows. The detail view must
// reserve the wrapped header's full height (> detailHeaderHeight) and shrink the
// body viewport accordingly, so header rows + viewport rows + status bar never
// exceed the terminal height (no top-scroll/overflow re-introduced).
func TestDetailHeaderHeightAccountsForWrappedTitle(t *testing.T) {
	m := newModel()
	longTitle := strings.Repeat("very long title word ", 10)
	items := []list.Item{
		item{number: 123, title: longTitle, body: "body", type_: "issue"},
	}
	m.issueList.SetItems(items)
	m.loading = false

	const w, h = 30, 24
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: h})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := tm.(model)

	hdrHeight := lipgloss.Height(mm.detailHeader())
	if hdrHeight <= detailHeaderHeight {
		t.Fatalf("expected wrapped header height > %d, got %d", detailHeaderHeight, hdrHeight)
	}

	if got := mm.detailViewport.Height; got != h-hdrHeight-statusBarHeight {
		t.Errorf("viewport height = %d, want %d (term - header - statusbar)",
			got, h-hdrHeight-statusBarHeight)
	}

	// Header + body viewport + status bar must fit within the terminal height.
	total := hdrHeight + mm.detailViewport.Height + statusBarHeight
	if total > h {
		t.Errorf("rendered detail height %d exceeds terminal height %d", total, h)
	}

	// The full title must still start at the top of the rendered view.
	firstLine := strings.SplitN(mm.View(), "\n", 2)[0]
	if !strings.Contains(firstLine, "#123") {
		t.Errorf("title not on first line of detail view: %q", firstLine)
	}
}

// On a short terminal, opening a detail view must keep the title at the top and
// not panic, even when the body is much taller than the pane.
func TestDetailOpensAtTop(t *testing.T) {
	m := newModel()
	long := strings.Repeat("line of body text\n", 100)
	items := []list.Item{
		item{number: 42, title: "important bug", body: long, type_: "issue"},
	}
	m.issueList.SetItems(items)
	m.loading = false

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 40, Height: 6})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})

	mm := tm.(model)
	if !mm.detailOpen {
		t.Fatal("detail did not open")
	}
	if !mm.detailViewport.AtTop() {
		t.Errorf("viewport not anchored at top on open (offset=%d)", mm.detailViewport.YOffset)
	}

	view := mm.View()
	firstLine := strings.SplitN(view, "\n", 2)[0]
	if !strings.Contains(firstLine, "#42") || !strings.Contains(firstLine, "important bug") {
		t.Errorf("title not on first line of detail view: %q", firstLine)
	}
}

// With a tall body, ctrl+n scrolls the detail viewport down one line and
// ctrl+p scrolls it back up one line.
func TestDetailScrollCtrlNP(t *testing.T) {
	m := newModel()
	long := strings.Repeat("line of body text\n", 100)
	items := []list.Item{
		item{number: 7, title: "scroll me", body: long, type_: "issue"},
	}
	m.issueList.SetItems(items)
	m.loading = false

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 40, Height: 10})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if off := tm.(model).detailViewport.YOffset; off != 0 {
		t.Fatalf("expected viewport at top, got offset=%d", off)
	}

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlN})
	if off := tm.(model).detailViewport.YOffset; off != 1 {
		t.Errorf("ctrl+n: want offset 1, got %d", off)
	}

	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	if off := tm.(model).detailViewport.YOffset; off != 0 {
		t.Errorf("ctrl+p: want offset 0, got %d", off)
	}
}

// A body shorter than the screen still renders without issue.
func TestDetailShortBody(t *testing.T) {
	m := newModel()
	items := []list.Item{
		item{number: 1, title: "tiny", body: "hello", type_: "issue"},
	}
	m.issueList.SetItems(items)
	m.loading = false

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})

	mm := tm.(model)
	if !strings.Contains(mm.View(), "hello") {
		t.Errorf("short body not rendered")
	}
}

// The detail title must be indented to column 2 so it aligns with list items
// (NormalTitle PaddingLeft(2)) and the rest of the app, and the indented block
// must still fit the terminal width without overflow even when the title wraps.
func TestDetailHeaderLeftMargin(t *testing.T) {
	m := newModel()
	items := []list.Item{
		item{number: 99, title: "aligned title", body: "body", type_: "issue"},
	}
	m.issueList.SetItems(items)
	m.loading = false

	const w, h = 80, 24
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: h})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := tm.(model)

	firstLine := strings.SplitN(mm.detailHeader(), "\n", 2)[0]
	if !strings.HasPrefix(firstLine, "  #99") {
		t.Errorf("detail header not indented to column 2: %q", firstLine)
	}

	// A long title that wraps must keep the indented block within the terminal
	// width (padding is subtracted from Width, not added on top).
	m2 := newModel()
	m2.issueList.SetItems([]list.Item{
		item{number: 100, title: strings.Repeat("very long title word ", 10), body: "b", type_: "issue"},
	})
	m2.loading = false
	var tm2 tea.Model = m2
	tm2, _ = tm2.Update(tea.WindowSizeMsg{Width: 30, Height: 24})
	tm2, _ = tm2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	hdr := tm2.(model).detailHeader()
	if gotW := lipgloss.Width(hdr); gotW > 30 {
		t.Errorf("wrapped indented header width %d exceeds terminal width 30", gotW)
	}
	for _, line := range strings.Split(hdr, "\n") {
		if !strings.HasPrefix(line, "  ") {
			t.Errorf("wrapped header line not indented to column 2: %q", line)
		}
	}
}
