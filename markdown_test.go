package main

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// A markdown body renders with formatting in the detail view: a heading is not
// shown as a literal "# ", list items get bullets, and a code fence/emphasis
// survive rendering rather than appearing as raw markup.
func TestDetailRendersMarkdown(t *testing.T) {
	body := "# Title\n\n- first item\n- second item\n\n```\ncode block\n```\n\nsome *emphasis* text"
	m := openDetailWithBody(t, body, 80, 24)

	plain := ansi.Strip(strings.Join(m.detailWrappedLines(), "\n"))

	if strings.Contains(plain, "# Title") {
		t.Errorf("heading rendered as literal markdown:\n%s", plain)
	}
	if !strings.Contains(plain, "Title") {
		t.Errorf("heading text missing from rendered body:\n%s", plain)
	}
	// glamour renders unordered list items with a bullet glyph.
	if !strings.Contains(plain, "•") {
		t.Errorf("list bullet missing from rendered body:\n%s", plain)
	}
	if !strings.Contains(plain, "first item") || !strings.Contains(plain, "second item") {
		t.Errorf("list item text missing from rendered body:\n%s", plain)
	}
	if !strings.Contains(plain, "code block") {
		t.Errorf("code fence content missing from rendered body:\n%s", plain)
	}
	// Emphasis markers should not survive as literal asterisks.
	if strings.Contains(plain, "*emphasis*") {
		t.Errorf("emphasis rendered as literal markdown:\n%s", plain)
	}
	if !strings.Contains(plain, "emphasis") {
		t.Errorf("emphasis text missing from rendered body:\n%s", plain)
	}
}

// Search over a markdown body finds matches and the reported match offsets land
// on the matched word in the *visible* (ANSI-stripped) rendered text, so the
// highlight aligns with what the user sees even though glamour injects ANSI
// escapes around the text.
func TestDetailSearchAlignsOnMarkdown(t *testing.T) {
	body := "# Heading\n\nThis paragraph mentions needle once.\n\n- needle in a list item"
	m := openDetailWithBody(t, body, 80, 24)

	var tm tea.Model = m
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	tm = typeRunes(tm, "needle")
	mm := tm.(model)

	if len(mm.detailMatches) != 2 {
		t.Fatalf("matches = %d, want 2", len(mm.detailMatches))
	}

	plain := mm.detailPlainLines()
	for i, mt := range mm.detailMatches {
		if mt.line < 0 || mt.line >= len(plain) {
			t.Fatalf("match %d on out-of-range line %d (have %d lines)", i, mt.line, len(plain))
		}
		runes := []rune(plain[mt.line])
		end := mt.startCol + mt.length
		if mt.startCol < 0 || end > len(runes) {
			t.Fatalf("match %d cols [%d,%d) out of range on line %q", i, mt.startCol, end, plain[mt.line])
		}
		got := string(runes[mt.startCol:end])
		if !strings.EqualFold(got, "needle") {
			t.Errorf("match %d at line %d cols [%d,%d) = %q, want %q", i, mt.line, mt.startCol, end, got, "needle")
		}
	}

	// The rendered (styled) content must still contain the active-match styling
	// and keep the matched word's letters intact.
	content := mm.detailBodyContent()
	if !strings.Contains(content, detailActiveMatchStyle.Render("n")) {
		t.Errorf("active match styling not present in rendered markdown content")
	}
	if strings.Count(ansi.Strip(content), "needle") != 2 {
		t.Errorf("rendered content lost a match after highlighting:\n%s", ansi.Strip(content))
	}
}

// renderMarkdown memoizes the most recent (body, width) so the two back-to-back
// projections per search keystroke / resize render through glamour at most once.
// This asserts: a repeat (body, width) is a cache hit returning identical output;
// a width change or a body change evicts the entry (so it never goes stale); and
// the cached output matches a fresh uncached render.
func TestRenderMarkdownMemoizesPerBodyWidth(t *testing.T) {
	mdRenderMu.Lock()
	mdRenderCacheOK = false
	mdRenderMu.Unlock()

	body := "# Title\n\nsome *emphasis* and a needle word."

	// First render populates the cache keyed by (body, 80).
	out1 := renderMarkdown(body, 80)
	mdRenderMu.Lock()
	if !mdRenderCacheOK || mdRenderCacheBody != body || mdRenderCacheW != 80 {
		mdRenderMu.Unlock()
		t.Fatalf("cache not populated for (body, 80): ok=%v body=%q w=%d", mdRenderCacheOK, mdRenderCacheBody, mdRenderCacheW)
	}
	mdRenderMu.Unlock()

	// Repeat call with the same (body, width) is a hit and returns identical output.
	if out2 := renderMarkdown(body, 80); out2 != out1 {
		t.Errorf("repeat render differs from first:\nfirst:  %q\nsecond: %q", out1, out2)
	}

	// The memoized output must equal a fresh uncached render.
	if want := renderMarkdownUncached(body, 80); out1 != want {
		t.Errorf("cached output != uncached render:\ncached:   %q\nuncached: %q", out1, want)
	}

	// A width change evicts the entry (new key) and re-renders.
	out80w := renderMarkdown(body, 40)
	mdRenderMu.Lock()
	if mdRenderCacheW != 40 || mdRenderCacheBody != body {
		mdRenderMu.Unlock()
		t.Fatalf("cache not re-keyed on width change: body=%q w=%d", mdRenderCacheBody, mdRenderCacheW)
	}
	mdRenderMu.Unlock()
	if want := renderMarkdownUncached(body, 40); out80w != want {
		t.Errorf("width-changed render != uncached:\ngot:  %q\nwant: %q", out80w, want)
	}

	// A body change (e.g. switching to a different issue/PR) evicts the entry too.
	other := "# Other\n\ndifferent body entirely."
	outOther := renderMarkdown(other, 40)
	mdRenderMu.Lock()
	if mdRenderCacheBody != other || mdRenderCacheW != 40 {
		mdRenderMu.Unlock()
		t.Fatalf("cache not re-keyed on body change: body=%q w=%d", mdRenderCacheBody, mdRenderCacheW)
	}
	mdRenderMu.Unlock()
	if want := renderMarkdownUncached(other, 40); outOther != want {
		t.Errorf("body-changed render != uncached:\ngot:  %q\nwant: %q", outOther, want)
	}
}

// An empty body and the loading placeholder are not run through markdown
// rendering and render sanely (no panic, expected text present).
func TestDetailEmptyAndLoadingBody(t *testing.T) {
	// Empty body.
	m := newModel()
	m.issueList.SetItems([]list.Item{item{number: 1, title: "t", body: "", type_: "issue"}})
	m.loading = false
	var tm tea.Model = m
	tm, _ = tm.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	tm, _ = tm.Update(tea.KeyMsg{Type: tea.KeyEnter})
	mm := tm.(model)
	// Empty-body items load lazily; force a settled empty state.
	mm.detailLoading = false
	if got := ansi.Strip(strings.Join(mm.detailWrappedLines(), "\n")); strings.TrimSpace(got) != "" {
		t.Errorf("empty body rendered non-empty content: %q", got)
	}

	// Loading placeholder.
	lm := openDetailWithBody(t, "anything", 80, 24)
	lm.detailLoading = true
	if got := ansi.Strip(strings.Join(lm.detailWrappedLines(), "\n")); !strings.Contains(got, "Loading body...") {
		t.Errorf("loading placeholder missing: %q", got)
	}
}
