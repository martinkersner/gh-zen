package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDetailViewportSize(t *testing.T) {
	cases := []struct {
		name         string
		w, h         int
		wantW, wantH int
	}{
		{"normal", 80, 24, 78, 21},
		{"tiny height", 80, 2, 78, 1},
		{"zero", 0, 0, 1, 1},
		{"negative", -5, -5, 1, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotW, gotH := detailViewportSize(c.w, c.h)
			if gotW != c.wantW || gotH != c.wantH {
				t.Errorf("detailViewportSize(%d,%d) = (%d,%d), want (%d,%d)",
					c.w, c.h, gotW, gotH, c.wantW, c.wantH)
			}
		})
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
