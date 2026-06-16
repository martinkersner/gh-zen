package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
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

// (TestDetailScrollCtrlNP removed — fully subsumed by
// TestDetailCtrlNPScrollsWhenNotSearching in detail_search_test.go, which
// asserts the same ctrl+n/ctrl+p one-line scroll behavior when not searching.)

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

// The detail title must be indented to column 1 so it aligns with list items
// (NormalTitle PaddingLeft(1)) and the rest of the app, and the indented block
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
	if !strings.HasPrefix(firstLine, " #99") {
		t.Errorf("detail header not indented to column 1: %q", firstLine)
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
		if !strings.HasPrefix(line, " ") {
			t.Errorf("wrapped header line not indented to column 1: %q", line)
		}
	}
}

func TestComposeDetailBodyNoComments(t *testing.T) {
	// With no comments the body must be returned verbatim so the empty-state and
	// loading handling in detailWrappedLines stay unchanged.
	if got := composeDetailBody("just the body", nil, 0); got != "just the body" {
		t.Errorf("composeDetailBody(no comments) = %q, want verbatim body", got)
	}
}

func TestComposeDetailBodyWithComments(t *testing.T) {
	out := composeDetailBody("the body", []comment{
		{author: "alice", body: "first"},
		{author: "bob", body: "second"},
	}, 2)
	// Body preserved at the top, comments appended under a Comments section in
	// order, each with a bold @author attribution.
	if !strings.HasPrefix(out, "the body") {
		t.Errorf("body not preserved at top:\n%s", out)
	}
	if !strings.Contains(out, "## Comments") {
		t.Errorf("missing Comments section:\n%s", out)
	}
	idxAlice := strings.Index(out, "**@alice**")
	idxBob := strings.Index(out, "**@bob**")
	if idxAlice < 0 || idxBob < 0 {
		t.Fatalf("missing author attributions:\n%s", out)
	}
	if idxAlice > idxBob {
		t.Errorf("comments out of order: alice should precede bob:\n%s", out)
	}
	if !strings.Contains(out, "first") || !strings.Contains(out, "second") {
		t.Errorf("comment bodies missing:\n%s", out)
	}
	// Exactly one horizontal rule: between the two comments, none before the
	// first (which sits flush under the heading).
	if n := strings.Count(out, "\n---\n"); n != 1 {
		t.Errorf("want 1 separator rule between 2 comments, got %d:\n%s", n, out)
	}
}

func TestComposeDetailBodySingleCommentNoRule(t *testing.T) {
	// A lone comment has no separator rule before it.
	out := composeDetailBody("b", []comment{{author: "alice", body: "only"}}, 1)
	if strings.Contains(out, "\n---\n") {
		t.Errorf("single comment should have no separator rule:\n%s", out)
	}
}

func TestComposeDetailBodyEmptyAuthor(t *testing.T) {
	out := composeDetailBody("b", []comment{{author: "", body: "ghost"}}, 1)
	if !strings.Contains(out, "**@(unknown)**") {
		t.Errorf("empty author not labeled as unknown:\n%s", out)
	}
}

func TestComposeDetailBodyTruncated(t *testing.T) {
	// When the thread's total exceeds the rendered comments, a truncation
	// indicator naming both counts is appended so the user knows more exist.
	out := composeDetailBody("b", []comment{
		{author: "alice", body: "one"},
		{author: "bob", body: "two"},
	}, 51)
	if !strings.Contains(out, "_(showing 2 of 51 comments)_") {
		t.Errorf("missing truncation indicator:\n%s", out)
	}
}

func TestComposeDetailBodyNotTruncated(t *testing.T) {
	// totalCount equal to the rendered count (and the typical totalCount==0 from
	// callers that don't supply it) must not add a truncation indicator — output
	// is identical to the non-truncated rendering.
	withTotal := composeDetailBody("b", []comment{{author: "alice", body: "one"}}, 1)
	zeroTotal := composeDetailBody("b", []comment{{author: "alice", body: "one"}}, 0)
	if strings.Contains(withTotal, "showing") {
		t.Errorf("unexpected truncation indicator when total==len:\n%s", withTotal)
	}
	if withTotal != zeroTotal {
		t.Errorf("totalCount==0 should render identically to total==len:\n%q\n%q", zeroTotal, withTotal)
	}
}

// --- label chips ---

func TestNormalizeHexColor(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"d73a4a", "#d73a4a"},  // bare GitHub color
		{"#d73a4a", "#d73a4a"}, // tolerate leading '#'
		{"", ""},               // empty
		{"abc", ""},            // too short
		{"d73a4a00", ""},       // too long
		{"gggggg", ""},         // non-hex digits
	}
	for _, c := range cases {
		if got := normalizeHexColor(c.in); got != c.want {
			t.Errorf("normalizeHexColor(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLabelTextColorContrast(t *testing.T) {
	// Light backgrounds get black text, dark backgrounds get white.
	if got := labelTextColor("#f9e2af"); got != "#000000" {
		t.Errorf("light bg text = %q, want #000000", got)
	}
	if got := labelTextColor("#0e8a16"); got != "#ffffff" {
		t.Errorf("dark bg text = %q, want #ffffff", got)
	}
}

func TestRenderLabelChipsEmpty(t *testing.T) {
	if got := renderLabelChips(nil, 80); got != "" {
		t.Errorf("renderLabelChips(nil) = %q, want empty", got)
	}
}

func TestRenderLabelChipsContainsNames(t *testing.T) {
	chips := renderLabelChips([]label{{name: "bug", color: "d73a4a"}, {name: "docs", color: "0075ca"}}, 80)
	plain := ansi.Strip(chips)
	if !strings.Contains(plain, "bug") || !strings.Contains(plain, "docs") {
		t.Errorf("chip row missing label names: %q", plain)
	}
	// Each chip is padded by one space on each side (Padding(0,1)), so the names
	// read as distinct chips rather than running together. (Color escapes are
	// omitted by lipgloss when no terminal color profile is detected, as under
	// `go test`, so we assert on structure, not ANSI.)
	if !strings.Contains(plain, " bug ") || !strings.Contains(plain, " docs ") {
		t.Errorf("chips not space-padded: %q", plain)
	}
}

// A detail item with labels renders a chip row beneath the title; the names are
// present in the header and the header grows taller than the title-only case.
func TestDetailHeaderRendersLabelChips(t *testing.T) {
	m := newModel()
	m.issueList.SetItems([]list.Item{
		item{number: 7, title: "labeled issue", body: "b", type_: "issue",
			labels: []label{{name: "bug", color: "d73a4a"}}},
	})
	m.loading = false

	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := tm.(model)

	hdr := mm.detailHeader()
	if !strings.Contains(ansi.Strip(hdr), "bug") {
		t.Errorf("detail header missing label chip:\n%s", ansi.Strip(hdr))
	}

	// Same item without labels: header omits the chip row and is shorter.
	m2 := newModel()
	m2.issueList.SetItems([]list.Item{
		item{number: 7, title: "labeled issue", body: "b", type_: "issue"},
	})
	m2.loading = false
	var tm2 tea.Model = m2
	tm2, _ = tm2.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm2, _ = tm2.Update(tea.KeyMsg{Type: tea.KeyEnter})
	noLabelHdr := tm2.(model).detailHeader()

	if strings.Contains(ansi.Strip(noLabelHdr), "bug") {
		t.Errorf("no-label header unexpectedly contains a chip:\n%s", noLabelHdr)
	}
	if lipgloss.Height(hdr) <= lipgloss.Height(noLabelHdr) {
		t.Errorf("labeled header height %d should exceed no-label header height %d",
			lipgloss.Height(hdr), lipgloss.Height(noLabelHdr))
	}
}

// A short label set that already fits the budget renders every chip unchanged
// from the unclamped output (no overflow marker, no dropped chips).
func TestRenderLabelChipsShortSetUnchanged(t *testing.T) {
	labels := []label{{name: "bug", color: "d73a4a"}, {name: "docs", color: "0075ca"}}
	clamped := renderLabelChips(labels, 80)
	unclamped := renderLabelChips(labels, 0) // 0 disables the clamp
	if clamped != unclamped {
		t.Errorf("short label set should render unchanged:\nclamped:   %q\nunclamped: %q", clamped, unclamped)
	}
	if strings.Contains(ansi.Strip(clamped), "+") {
		t.Errorf("short label set should have no overflow marker: %q", ansi.Strip(clamped))
	}
}

// Many/long labels in a narrow budget must keep the chip row within maxWidth
// with no mid-chip wrap and surface a "+N" overflow marker for the dropped
// chips.
func TestRenderLabelChipsOverflowFits(t *testing.T) {
	labels := []label{
		{name: "needs-triage", color: "d73a4a"},
		{name: "enhancement", color: "0075ca"},
		{name: "documentation", color: "0e8a16"},
		{name: "good-first-issue", color: "7057ff"},
		{name: "help-wanted", color: "008672"},
		{name: "wontfix", color: "ffffff"},
	}
	const budget = 30
	chips := renderLabelChips(labels, budget)
	if w := lipgloss.Width(chips); w > budget {
		t.Errorf("chip row width %d exceeds budget %d:\n%q", w, budget, ansi.Strip(chips))
	}
	// The row must not wrap to a second line (no mid-chip wrap).
	if strings.Contains(chips, "\n") {
		t.Errorf("chip row wrapped to multiple lines:\n%q", chips)
	}
	// Some chips were dropped, so a "+N" marker must be present.
	if !strings.Contains(ansi.Strip(chips), "+") {
		t.Errorf("expected +N overflow marker, got: %q", ansi.Strip(chips))
	}
}

// Even when a single chip is wider than the whole budget the function must
// return without panicking and produce a single (non-wrapping) line rather
// than emitting nothing.
func TestRenderLabelChipsSingleChipWiderThanBudget(t *testing.T) {
	labels := []label{{name: strings.Repeat("x", 40), color: "d73a4a"}}
	chips := renderLabelChips(labels, 10)
	if chips == "" {
		t.Errorf("single oversized chip should still render something")
	}
	if strings.Contains(chips, "\n") {
		t.Errorf("single oversized chip should not wrap:\n%q", chips)
	}
}

// End to end through detailHeader at a narrow terminal width: the whole header
// (title block + chip row) stays within m.width even with many long labels.
func TestDetailHeaderLabelChipsWidthClamped(t *testing.T) {
	m := newModel()
	m.issueList.SetItems([]list.Item{
		item{number: 42, title: "wide labels", body: "b", type_: "issue", labels: []label{
			{name: "needs-triage", color: "d73a4a"},
			{name: "enhancement", color: "0075ca"},
			{name: "documentation", color: "0e8a16"},
			{name: "good-first-issue", color: "7057ff"},
			{name: "help-wanted", color: "008672"},
		}},
	})
	m.loading = false

	const w = 28
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: w, Height: 24})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	hdr := tm.(model).detailHeader()

	for _, line := range strings.Split(hdr, "\n") {
		if gotW := lipgloss.Width(line); gotW > w {
			t.Errorf("detail header line width %d exceeds terminal width %d:\n%q", gotW, w, line)
		}
	}
}
