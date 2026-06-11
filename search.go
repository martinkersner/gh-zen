package main

import (
	"strings"
)

// searchMatch locates one occurrence of the in-detail search query inside the
// rendered (word-wrapped) detail body. line is the 0-based index into the
// wrapped lines; startCol/length are rune offsets into that line, so the render
// path can highlight exactly the matched run.
type searchMatch struct {
	line     int
	startCol int
	length   int
}

// findMatches returns every contiguous, case-insensitive occurrence of query
// within the given wrapped lines, in reading order (top-to-bottom, left-to-
// right). It reuses the same substring semantics as the list filter
// (case-insensitive, contiguous - not subsequence) via runeIndex. An empty
// query yields no matches. Matches do not overlap: after a hit the scan
// resumes past the matched run.
func findMatches(lines []string, query string) []searchMatch {
	queryRunes := []rune(strings.ToLower(query))
	if len(queryRunes) == 0 {
		return nil
	}
	var matches []searchMatch
	for li, line := range lines {
		lineRunes := []rune(strings.ToLower(line))
		offset := 0
		for offset+len(queryRunes) <= len(lineRunes) {
			idx := runeIndex(lineRunes[offset:], queryRunes)
			if idx < 0 {
				break
			}
			start := offset + idx
			matches = append(matches, searchMatch{line: li, startCol: start, length: len(queryRunes)})
			offset = start + len(queryRunes)
		}
	}
	return matches
}

// nextMatchIndex returns the index of the next match after cur, wrapping around
// to 0 past the end. For an empty match set it returns 0.
func nextMatchIndex(cur, n int) int {
	if n <= 0 {
		return 0
	}
	return (cur + 1) % n
}

// prevMatchIndex returns the index of the previous match before cur, wrapping
// around to the last match below 0. For an empty match set it returns 0.
func prevMatchIndex(cur, n int) int {
	if n <= 0 {
		return 0
	}
	return (cur - 1 + n) % n
}

// scrollOffsetFor returns the viewport YOffset needed to keep the line at
// matchLine visible within a window of viewportHeight lines, given the current
// offset. If the line is already visible the current offset is returned
// unchanged; otherwise it scrolls just enough to bring the line to the top (when
// above the window) or bottom (when below). The result is clamped to
// [0, maxOffset].
func scrollOffsetFor(matchLine, currentOffset, viewportHeight, maxOffset int) int {
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	offset := currentOffset
	if matchLine < currentOffset {
		offset = matchLine
	} else if matchLine >= currentOffset+viewportHeight {
		offset = matchLine - viewportHeight + 1
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	if offset < 0 {
		offset = 0
	}
	return offset
}
