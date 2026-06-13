package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// The accent color the number prefix is rendered in, as the truecolor SGR
// sequence lipgloss emits for lipgloss.Color("#7aa2f7").
const numberAccentSGR = "38;2;121;162;247"

// numberPrefixLen identifies the "#<number> " span (including the trailing
// space) so only the number is recolored.
func TestNumberPrefixLen(t *testing.T) {
	cases := []struct {
		title string
		want  int
	}{
		{"#123 hello world", 5}, // "#123 "
		{"#1 a", 3},             // "#1 "
		{"#42", 3},              // no space: whole string is the prefix
		{"no prefix", 0},        // does not start with '#'
		{"", 0},
	}
	for _, c := range cases {
		if got := numberPrefixLen(c.title); got != c.want {
			t.Errorf("numberPrefixLen(%q) = %d, want %d", c.title, got, c.want)
		}
	}
}

// renderTitle must color the "#<number>" prefix with the accent while the title
// text uses the row's own (different) foreground, in both normal and selected
// rows. We assert the accent SGR appears and that it is confined to the prefix.
func TestRenderTitleColorsNumberPrefix(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	s := list.NewDefaultItemStyles()
	number := lipgloss.NewStyle().Foreground(lipgloss.Color("#7aa2f7")).Inline(true)
	title := "#123 hello"
	prefixLen := numberPrefixLen(title)

	for _, tc := range []struct {
		name string
		row  lipgloss.Style
	}{
		{"normal", s.NormalTitle},
		{"selected", s.SelectedTitle},
	} {
		out := renderTitle(title, prefixLen, nil, false, tc.row, s.FilterMatch, number)
		if !strings.Contains(out, numberAccentSGR) {
			t.Errorf("%s: rendered title missing number accent %q: %q", tc.name, numberAccentSGR, out)
		}
		// The accent must not bleed into the title text: everything after the
		// last accent code, up to the title body, should not re-open the accent
		// once the title word starts. We assert the plain text is intact and the
		// accent precedes "hello" rather than wrapping it.
		plain := stripANSI(out)
		if !strings.Contains(plain, "#123 hello") {
			t.Errorf("%s: plain text not intact: %q", tc.name, plain)
		}
		// The title word "hello" must be styled with the row foreground, not the
		// accent: the accent SGR must appear before the title body's color, and
		// the segment rendering "hello" must not carry the accent.
		idx := strings.Index(out, "hello")
		if idx >= 0 && strings.Contains(out[idx:], numberAccentSGR) {
			t.Errorf("%s: number accent leaked into title body: %q", tc.name, out[idx:])
		}
	}
}

// FilterValue stays plain (no ANSI) so substring filtering keeps matching on
// "#<number> title".
func TestFilterValuePlain(t *testing.T) {
	it := item{number: 7, title: "fix thing", type_: "issue"}
	if got := it.FilterValue(); got != "#7 fix thing" {
		t.Errorf("FilterValue() = %q, want %q", got, "#7 fix thing")
	}
	if strings.ContainsRune(it.FilterValue(), '\x1b') {
		t.Errorf("FilterValue() must not contain ANSI: %q", it.FilterValue())
	}
}

// stripANSI removes SGR escape sequences for plain-text assertions.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			// skip until 'm'
			for i < len(s) && s[i] != 'm' {
				i++
			}
			if i < len(s) {
				i++ // skip 'm'
			}
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
