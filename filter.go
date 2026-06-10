package main

import (
	"strings"

	"github.com/charmbracelet/bubbles/list"
)

// substringFilter is a list.FilterFunc that keeps only targets containing the
// term as a contiguous, case-insensitive substring. Unlike the default fuzzy
// filter it does NOT match subsequences (e.g. "apa" does not match "alpha").
//
// MatchedIndexes are reported as rune indexes into the target so the list
// delegate can highlight the matched run. The list only calls this with a
// non-empty term; an empty term is handled by the list itself (shows all).
func substringFilter(term string, targets []string) []list.Rank {
	termRunes := []rune(strings.ToLower(term))
	if len(termRunes) == 0 {
		// Defensive: the list never calls us with an empty term, but if it
		// did, treat every target as a (trivial) match.
		ranks := make([]list.Rank, len(targets))
		for i := range targets {
			ranks[i] = list.Rank{Index: i}
		}
		return ranks
	}

	var ranks []list.Rank
	for i, target := range targets {
		targetRunes := []rune(strings.ToLower(target))
		start := runeIndex(targetRunes, termRunes)
		if start < 0 {
			continue
		}
		matched := make([]int, len(termRunes))
		for j := range termRunes {
			matched[j] = start + j
		}
		ranks = append(ranks, list.Rank{Index: i, MatchedIndexes: matched})
	}
	return ranks
}

// runeIndex returns the index of the first contiguous occurrence of sub within
// s (both rune slices), or -1 if not present. sub must be non-empty.
func runeIndex(s, sub []rune) int {
	if len(sub) > len(s) {
		return -1
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := range sub {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}
