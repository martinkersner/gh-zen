package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
)

// A long title must truncate at the same visible column whether or not the row
// is selected. SelectedTitle and NormalTitle have different padding (0 vs 1) but
// SelectedTitle adds a 1-col left border, so both have the same horizontal frame
// size; deriving textwidth per-row-style keeps the truncated text identical.
// Regression test for issue #72.
func TestRenderTruncatesSameColumnSelectedAndNormal(t *testing.T) {
	d := newItemDelegate()
	const width = 30
	long := "#1 " + strings.Repeat("x", 200)
	items := []list.Item{
		item{number: 1, title: long, type_: "issue"},
		item{number: 2, title: long, type_: "issue"},
	}

	render := func(selectedIndex int) string {
		l := list.New(items, d, width, 24)
		l.Select(selectedIndex)
		var buf bytes.Buffer
		// Render row 0: selected when selectedIndex==0, normal otherwise.
		d.Render(&buf, l, 0, items[0])
		return buf.String()
	}

	// Extract the visible title text (strip ANSI, drop the row frame: leading
	// padding spaces and the selected row's left border rune).
	titleText := func(out string) string {
		plain := stripANSI(out)
		plain = strings.TrimLeft(plain, " │")
		return strings.TrimRight(plain, " ")
	}

	selected := titleText(render(0)) // row 0 is selected
	normal := titleText(render(1))   // row 0 is normal
	if selected != normal {
		t.Errorf("truncated title differs by selection:\n selected=%q (len %d)\n normal  =%q (len %d)",
			selected, len([]rune(selected)), normal, len([]rune(normal)))
	}
	if !strings.HasSuffix(selected, "…") {
		t.Errorf("expected long title to be truncated with ellipsis, got %q", selected)
	}
}
