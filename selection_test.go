package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The selected row must be marked by color alone — no left vertical bar / border
// glyph (issue #132). We render the same row as selected and as normal and
// assert: (1) neither output contains the bubbles-default left-border rune '│',
// and (2) the selected and normal outputs differ (selection is visible) and the
// difference is a foreground-color SGR, not a border character.
func TestSelectedRowHasNoBorderAndDiffersByColor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	d := newItemDelegate()
	const width = 40
	items := []list.Item{
		item{number: 1, title: "#1 alpha", type_: "issue"},
		item{number: 2, title: "#2 beta", type_: "issue"},
	}

	render := func(selectedIndex int) string {
		l := list.New(items, d, width, 24)
		l.Select(selectedIndex)
		var buf bytes.Buffer
		d.Render(&buf, l, 0, items[0]) // row 0: selected when selectedIndex==0
		return buf.String()
	}

	selected := render(0)
	normal := render(1)

	// No vertical bar / border glyph in either rendering.
	for name, out := range map[string]string{"selected": selected, "normal": normal} {
		if strings.ContainsRune(out, '│') {
			t.Errorf("%s row unexpectedly contains a border glyph '│': %q", name, out)
		}
	}

	// Selection must be visible: selected vs normal differ.
	if selected == normal {
		t.Fatalf("selected and normal rows render identically; selection not distinguishable:\n%q", selected)
	}

	// The visible text is identical (same item, no shift); only styling differs,
	// confirming selection is color-based rather than a structural border char.
	if ps, pn := stripANSI(selected), stripANSI(normal); ps != pn {
		t.Errorf("plain text differs between selected/normal (expected color-only diff):\n selected=%q\n normal  =%q", ps, pn)
	}
}

// Removing the SelectedTitle border (issue #132) must preserve the #122 no-shift
// invariant: SelectedTitle, NormalTitle, and DimmedTitle all have horizontal
// frame size 1, so the list content never shifts horizontally on cursor move.
func TestRowStylesShareHorizontalFrameSize(t *testing.T) {
	d := newItemDelegate()
	s := d.styles
	want := s.NormalTitle.GetHorizontalFrameSize()
	if want != 1 {
		t.Errorf("NormalTitle horizontal frame size = %d, want 1", want)
	}
	if got := s.SelectedTitle.GetHorizontalFrameSize(); got != want {
		t.Errorf("SelectedTitle horizontal frame size = %d, want %d (no-shift invariant)", got, want)
	}
	if got := s.DimmedTitle.GetHorizontalFrameSize(); got != want {
		t.Errorf("DimmedTitle horizontal frame size = %d, want %d", got, want)
	}
	// And no border on the selected row.
	if s.SelectedTitle.GetBorderLeft() {
		t.Error("SelectedTitle still has a left border; expected it removed (#132)")
	}
}
