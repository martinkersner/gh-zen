package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// renderRow renders the single item at index 0 of a width-w list and returns the
// raw (ANSI-carrying) output.
func renderRow(t *testing.T, it item, width int) string {
	t.Helper()
	d := newItemDelegate()
	l := list.New([]list.Item{it}, d, width, 24)
	var buf bytes.Buffer
	d.Render(&buf, l, 0, it)
	return buf.String()
}

// A row with an author shows "@author" right-aligned, separated from the title by
// at least rowAuthorGap blank columns, and colored with the accent so it reads as
// distinct from the title text.
func TestRenderRowShowsAuthorRightAligned(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	rebuildThemeStyles()

	const width = 40
	out := renderRow(t, item{number: 7, title: "short title", type_: "issue", author: "octocat"}, width)
	plain := strings.TrimRight(stripANSI(out), " ")

	if !strings.HasSuffix(plain, "@octocat") {
		t.Errorf("author not right-aligned at row end: %q", plain)
	}
	titleIdx := strings.Index(plain, "#7 short title")
	if titleIdx < 0 {
		t.Fatalf("title not intact: %q", plain)
	}
	// Title and author must be separated by at least rowAuthorGap spaces.
	gap := plain[titleIdx+len("#7 short title") : len(plain)-len("@octocat")]
	if strings.Trim(gap, " ") != "" || len(gap) < rowAuthorGap {
		t.Errorf("expected >=%d spaces between title and author, got %q", rowAuthorGap, gap)
	}
	// The author segment must carry the accent color (distinct from the title).
	if !strings.Contains(out, "@octocat") {
		t.Fatalf("author missing from raw output: %q", out)
	}
	if !strings.Contains(out, numberAccentSGR) {
		t.Errorf("author/number accent color %q missing: %q", numberAccentSGR, out)
	}
}

// When the row is too narrow for both the title and the author, the author wins:
// the title is truncated (with an ellipsis) to make room, and the author still
// renders in full.
func TestRenderRowAuthorWinsWhenNarrow(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	rebuildThemeStyles()

	const width = 20
	long := strings.Repeat("x", 100)
	out := renderRow(t, item{number: 7, title: long, type_: "issue", author: "octocat"}, width)
	plain := strings.TrimRight(stripANSI(out), " ")

	if !strings.HasSuffix(plain, "@octocat") {
		t.Errorf("author dropped on narrow row: %q", plain)
	}
	if !strings.Contains(plain, "…") {
		t.Errorf("title should be truncated with an ellipsis: %q", plain)
	}
	// The whole row must not exceed the terminal width.
	if w := ansi.StringWidth(plain); w > width {
		t.Errorf("row width %d exceeds terminal width %d: %q", w, width, plain)
	}
}

// A row without an author renders exactly as before — no trailing "@", no
// reserved right-side gap.
func TestRenderRowNoAuthor(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	rebuildThemeStyles()

	out := renderRow(t, item{number: 7, title: "hello", type_: "issue"}, 40)
	plain := strings.TrimRight(stripANSI(out), " ")
	if strings.Contains(plain, "@") {
		t.Errorf("unexpected author marker on authorless row: %q", plain)
	}
	if !strings.HasSuffix(plain, "hello") {
		t.Errorf("title not rendered flush without author: %q", plain)
	}
}
