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
