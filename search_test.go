package main

import (
	"reflect"
	"testing"
)

// searchBarLeft is the single source of truth for the slash-prefixed query the
// status bar shows for both the list filter and the in-detail search.
func TestSearchBarLeft(t *testing.T) {
	tests := []struct {
		query  string
		typing bool
		want   string
	}{
		{"", true, "/"},         // live, editable input: bare slash
		{"", false, ""},         // applied with no query: nothing
		{"beta", true, "/beta"}, // typing a query
		{"beta", false, "/beta"},
	}
	for _, tt := range tests {
		if got := searchBarLeft(tt.query, tt.typing); got != tt.want {
			t.Errorf("searchBarLeft(%q, %v) = %q, want %q", tt.query, tt.typing, got, tt.want)
		}
	}
}

func TestFindMatches(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		query string
		want  []searchMatch
	}{
		{
			name:  "empty query yields nothing",
			lines: []string{"hello world"},
			query: "",
			want:  nil,
		},
		{
			name:  "single match",
			lines: []string{"the quick brown fox"},
			query: "quick",
			want:  []searchMatch{{line: 0, startCol: 4, length: 5}},
		},
		{
			name:  "case insensitive",
			lines: []string{"The Quick BROWN fox"},
			query: "brown",
			want:  []searchMatch{{line: 0, startCol: 10, length: 5}},
		},
		{
			name:  "multiple non-overlapping on one line",
			lines: []string{"aa bb aa cc aa"},
			query: "aa",
			want: []searchMatch{
				{line: 0, startCol: 0, length: 2},
				{line: 0, startCol: 6, length: 2},
				{line: 0, startCol: 12, length: 2},
			},
		},
		{
			name:  "matches across lines in reading order",
			lines: []string{"foo bar", "baz foo"},
			query: "foo",
			want: []searchMatch{
				{line: 0, startCol: 0, length: 3},
				{line: 1, startCol: 4, length: 3},
			},
		},
		{
			name:  "overlapping pattern matched non-overlapping",
			lines: []string{"aaaa"},
			query: "aa",
			want: []searchMatch{
				{line: 0, startCol: 0, length: 2},
				{line: 0, startCol: 2, length: 2},
			},
		},
		{
			name:  "no match",
			lines: []string{"hello", "world"},
			query: "xyz",
			want:  nil,
		},
		{
			name:  "subsequence does not match (contiguous only)",
			lines: []string{"alpha"},
			query: "apa",
			want:  nil,
		},
		{
			// Multibyte: offsets must be in RUNES, not bytes. "héllo" (the é is two
			// bytes) appears at rune cols 0 and 12; a byte-offset regression would
			// report the wrong startCol here.
			name:  "multibyte rune offsets",
			lines: []string{"héllo wörld héllo"},
			query: "héllo",
			want: []searchMatch{
				{line: 0, startCol: 0, length: 5},
				{line: 0, startCol: 12, length: 5},
			},
		},
		{
			// Case-insensitive match over multibyte runes.
			name:  "multibyte case insensitive",
			lines: []string{"CAFÉ münchen"},
			query: "café",
			want:  []searchMatch{{line: 0, startCol: 0, length: 4}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := findMatches(tt.lines, tt.query)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("findMatches(%q) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestNextPrevMatchIndexWrap(t *testing.T) {
	// Forward through 3 matches wraps 0->1->2->0.
	cur := 0
	wantNext := []int{1, 2, 0}
	for _, w := range wantNext {
		cur = nextMatchIndex(cur, 3)
		if cur != w {
			t.Fatalf("nextMatchIndex wrap: got %d, want %d", cur, w)
		}
	}
	// Backward wraps 0->2->1->0.
	cur = 0
	wantPrev := []int{2, 1, 0}
	for _, w := range wantPrev {
		cur = prevMatchIndex(cur, 3)
		if cur != w {
			t.Fatalf("prevMatchIndex wrap: got %d, want %d", cur, w)
		}
	}
	// Empty match set is a no-op (returns 0).
	if got := nextMatchIndex(0, 0); got != 0 {
		t.Errorf("nextMatchIndex empty: got %d, want 0", got)
	}
	if got := prevMatchIndex(0, 0); got != 0 {
		t.Errorf("prevMatchIndex empty: got %d, want 0", got)
	}
}

func TestScrollOffsetFor(t *testing.T) {
	cases := []struct {
		name                                      string
		matchLine, curOffset, vpHeight, maxOffset int
		want                                      int
	}{
		{"already visible: unchanged", 5, 3, 10, 100, 3},
		{"above window: scroll up to line", 1, 5, 10, 100, 1},
		{"below window: scroll so line is last", 20, 0, 10, 100, 11},
		{"clamped to maxOffset", 99, 0, 10, 12, 12},
		{"clamped to zero", -3, 0, 10, 100, 0},
		{"at top edge visible", 0, 0, 10, 100, 0},
		{"at bottom edge visible", 9, 0, 10, 100, 0},
		{"just past bottom edge", 10, 0, 10, 100, 1},
		// viewportHeight < 1 is clamped to 1 (guard): a degenerate/zero-height
		// viewport must not divide the window away. With height treated as 1, line
		// 5 sits below the 1-row window so it scrolls so the line is last: 5-1+1=5.
		{"zero viewport height clamps to 1", 5, 0, 0, 100, 5},
		{"negative viewport height clamps to 1", 3, 0, -4, 100, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := scrollOffsetFor(c.matchLine, c.curOffset, c.vpHeight, c.maxOffset)
			if got != c.want {
				t.Errorf("scrollOffsetFor(%d,%d,%d,%d) = %d, want %d",
					c.matchLine, c.curOffset, c.vpHeight, c.maxOffset, got, c.want)
			}
		})
	}
}
