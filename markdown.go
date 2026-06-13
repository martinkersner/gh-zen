package main

import (
	"strings"
	"sync"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"
)

// glamour renderer construction parses a full style + word-wrap config and is
// relatively expensive, so renderers are cached per word-wrap width and reused
// across View calls. The width only changes on terminal resize, so the cache
// stays tiny in practice. A mutex guards it because View can run from a
// different goroutine than Update under bubbletea.
var (
	mdRendererMu    sync.Mutex
	mdRendererCache = map[int]*glamour.TermRenderer{}
)

// markdownRenderer returns a glamour renderer that word-wraps at width and uses
// the "dark" style to match the codebase's fixed Tokyo Night dark palette (see
// the lipgloss.Color usages in model.go; there is no light-mode branch). A nil
// renderer (construction failure) is treated as "no markdown" by the caller.
func markdownRenderer(width int) *glamour.TermRenderer {
	if width < 1 {
		width = 1
	}
	mdRendererMu.Lock()
	defer mdRendererMu.Unlock()
	if r, ok := mdRendererCache[width]; ok {
		return r
	}
	r, err := glamour.NewTermRenderer(
		glamour.WithStandardStyle("dark"),
		glamour.WithWordWrap(width),
	)
	if err != nil {
		return nil
	}
	mdRendererCache[width] = r
	return r
}

// A single in-detail search keystroke (or resize) projects the body two ways
// from the same render — the plain text for findMatches and the styled lines
// for highlighting — so renderMarkdown is called twice back-to-back with the
// identical (body, width). The glamour render is the expensive part, so the
// most recent result is memoized keyed by (body, width): the second call of a
// pair is a cache hit, and any body/width change makes a new key (which evicts
// the previous one), so the cache stays a single entry and never goes stale.
var (
	mdRenderMu        sync.Mutex
	mdRenderCacheBody string
	mdRenderCacheW    int
	mdRenderCacheOut  string
	mdRenderCacheOK   bool
)

// renderMarkdown renders GitHub-flavored markdown to styled terminal output
// (containing ANSI escapes) word-wrapped to width. On any rendering failure it
// falls back to the raw body so the detail view never goes blank. glamour adds
// surrounding blank lines and a trailing newline; those are trimmed so the
// body sits flush at the top of the viewport like the previous plain wrapping.
//
// The result is memoized for the most recent (body, width) so the doubled call
// per search keystroke / resize renders through glamour at most once.
func renderMarkdown(body string, width int) string {
	mdRenderMu.Lock()
	if mdRenderCacheOK && mdRenderCacheW == width && mdRenderCacheBody == body {
		out := mdRenderCacheOut
		mdRenderMu.Unlock()
		return out
	}
	mdRenderMu.Unlock()

	out := renderMarkdownUncached(body, width)

	mdRenderMu.Lock()
	mdRenderCacheBody = body
	mdRenderCacheW = width
	mdRenderCacheOut = out
	mdRenderCacheOK = true
	mdRenderMu.Unlock()
	return out
}

// renderMarkdownUncached performs the actual glamour render (no memoization).
func renderMarkdownUncached(body string, width int) string {
	r := markdownRenderer(width)
	if r == nil {
		return body
	}
	out, err := r.Render(body)
	if err != nil {
		return body
	}
	return strings.Trim(out, "\n")
}

// highlightStyledLine overlays the search-match styling onto a line that may
// already contain glamour's ANSI escapes. active/other map *visible* rune
// columns (the same offsets findMatches computes over the ANSI-stripped line)
// to whether that column is part of the active / a non-active match.
//
// It walks the line one decoded sequence at a time via ansi.DecodeSequence:
// escape sequences (width 0) are emitted verbatim so glamour's formatting is
// preserved and do not advance the column; printable graphemes advance the
// column by their rune count (matching findMatches' []rune model) and, when a
// column is part of a match, are wrapped in the match style. Styling each
// matched rune individually keeps offsets aligned even when a match straddles a
// glamour style boundary.
func highlightStyledLine(line string, active, other map[int]bool) string {
	var b strings.Builder
	var state byte
	col := 0
	rest := line
	for len(rest) > 0 {
		seq, width, n, newState := ansi.DecodeSequence(rest, state, nil)
		state = newState
		if width == 0 {
			// Control / escape sequence: pass through, no visible column.
			b.WriteString(seq)
			rest = rest[n:]
			continue
		}
		// Printable: style each rune of the grapheme by its column.
		for _, r := range seq {
			switch {
			case active[col]:
				b.WriteString(detailActiveMatchStyle.Render(string(r)))
			case other[col]:
				b.WriteString(detailMatchStyle.Render(string(r)))
			default:
				b.WriteRune(r)
			}
			col++
		}
		rest = rest[n:]
	}
	return b.String()
}
