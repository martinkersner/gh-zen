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

	// The active match (index 0) must carry the active style while the OTHER match
	// (index 1) carries the non-active style — not merely "some rune is active-
	// styled somewhere". Verifying both distinguishes the active match from the
	// rest, which the prior single-rune check did not.
	if mm.detailActiveMatch != 0 {
		t.Fatalf("active match = %d, want 0 (first hit)", mm.detailActiveMatch)
	}
	content := mm.detailBodyContent()
	if !strings.Contains(content, detailActiveMatchStyle.Render("n")) {
		t.Errorf("active-match styling not present in rendered markdown content")
	}
	if !strings.Contains(content, detailMatchStyle.Render("n")) {
		t.Errorf("non-active (other) match styling not present; the second match was not styled as a plain match")
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

	// renders counts the underlying glamour renders since the last reset, so we
	// can prove a cache hit performs ZERO additional renders (a deterministic
	// renderer would return identical output even with caching removed, so output
	// equality alone can't distinguish a hit from a re-render).
	renders := func() int { return mdUncachedRenders }

	// First render populates the cache keyed by (body, 80) and performs one render.
	start := renders()
	out1 := renderMarkdown(body, 80)
	if got := renders() - start; got != 1 {
		t.Fatalf("first render performed %d glamour renders, want 1", got)
	}
	mdRenderMu.Lock()
	if !mdRenderCacheOK || mdRenderCacheBody != body || mdRenderCacheW != 80 {
		mdRenderMu.Unlock()
		t.Fatalf("cache not populated for (body, 80): ok=%v body=%q w=%d", mdRenderCacheOK, mdRenderCacheBody, mdRenderCacheW)
	}
	mdRenderMu.Unlock()

	// Repeat call with the same (body, width) is a HIT: identical output AND no
	// additional glamour render. This is the assertion that actually fails if the
	// memoization is removed.
	before := renders()
	out2 := renderMarkdown(body, 80)
	if out2 != out1 {
		t.Errorf("repeat render differs from first:\nfirst:  %q\nsecond: %q", out1, out2)
	}
	if got := renders() - before; got != 0 {
		t.Errorf("repeat (body, 80) render performed %d glamour renders, want 0 (cache miss)", got)
	}

	// A width change evicts the entry (new key) and performs a fresh render.
	before = renders()
	renderMarkdown(body, 40)
	if got := renders() - before; got != 1 {
		t.Errorf("width change performed %d glamour renders, want 1 (eviction)", got)
	}
	mdRenderMu.Lock()
	if mdRenderCacheW != 40 || mdRenderCacheBody != body {
		mdRenderMu.Unlock()
		t.Fatalf("cache not re-keyed on width change: body=%q w=%d", mdRenderCacheBody, mdRenderCacheW)
	}
	mdRenderMu.Unlock()
	// The previous (body, 80) entry is now evicted, so re-requesting it renders again
	// (a single-entry cache, not stale).
	before = renders()
	if got := renderMarkdown(body, 80); got != out1 {
		t.Errorf("re-render of evicted (body, 80) differs: %q vs %q", got, out1)
	}
	if got := renders() - before; got != 1 {
		t.Errorf("evicted (body, 80) re-request performed %d renders, want 1", got)
	}

	// A body change (e.g. switching to a different issue/PR) evicts the entry too.
	other := "# Other\n\ndifferent body entirely."
	before = renders()
	renderMarkdown(other, 80)
	if got := renders() - before; got != 1 {
		t.Errorf("body change performed %d glamour renders, want 1 (eviction)", got)
	}
	mdRenderMu.Lock()
	if mdRenderCacheBody != other || mdRenderCacheW != 80 {
		mdRenderMu.Unlock()
		t.Fatalf("cache not re-keyed on body change: body=%q w=%d", mdRenderCacheBody, mdRenderCacheW)
	}
	mdRenderMu.Unlock()
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
