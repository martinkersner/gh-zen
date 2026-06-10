package main

import (
	"reflect"
	"testing"
)

// matchedIndexSet returns the set of original target indexes returned by the
// filter for the given term.
func matchedIndexSet(term string, targets []string) map[int]bool {
	out := map[int]bool{}
	for _, r := range substringFilter(term, targets) {
		out[r.Index] = true
	}
	return out
}

func TestSubstringFilter(t *testing.T) {
	// Targets mirror item.FilterValue() = "#<number> <title>".
	targets := []string{
		"#1 alpha",      // 0
		"#2 alfa-two",   // 1
		"#3 alfa-three", // 2
		"#4 beta",       // 3
		"#42 gamma",     // 4
	}

	tests := []struct {
		name string
		term string
		want []int // expected original indexes (order-independent)
	}{
		{"substring in title", "lph", []int{0}},
		{"case insensitive", "LPH", []int{0}},
		{"contiguous substring across two", "alfa", []int{1, 2}},
		// Regression: subsequence must NOT match. "apa" is a subsequence of
		// "alpha" but not a contiguous substring.
		{"subsequence does not match", "apa", nil},
		// "alf" is contiguous in "alfa-*" but NOT in "alpha".
		{"alf matches alfa not alpha", "alf", []int{1, 2}},
		{"no match", "zzz", nil},
		// Number search: "42" matches #42 only.
		{"number 42", "42", []int{4}},
		// Bare digit "2" matches #2 and the "2" inside #42 (acceptable
		// substring behavior); assert the intended #2 item is included.
		{"digit 2 includes item 2", "2", []int{1, 4}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchedIndexSet(tt.term, targets)
			want := map[int]bool{}
			for _, i := range tt.want {
				want[i] = true
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("term %q: got indexes %v, want %v", tt.term, got, want)
			}
		})
	}
}

// TestSubstringFilterMatchedIndexes verifies matched rune indexes point at the
// contiguous run where the term occurs, so highlighting lines up.
func TestSubstringFilterMatchedIndexes(t *testing.T) {
	targets := []string{"#1 alpha"}
	ranks := substringFilter("lph", targets)
	if len(ranks) != 1 {
		t.Fatalf("expected 1 rank, got %d", len(ranks))
	}
	// "#1 alpha" -> 'l' at rune index 4, 'p' 5, 'h' 6.
	want := []int{4, 5, 6}
	if !reflect.DeepEqual(ranks[0].MatchedIndexes, want) {
		t.Errorf("matched indexes = %v, want %v", ranks[0].MatchedIndexes, want)
	}
}

// TestSubstringFilterEmptyTerm documents defensive behavior: the list never
// calls the filter with an empty term (it shows all items itself), but if it
// did, every target is returned.
func TestSubstringFilterEmptyTerm(t *testing.T) {
	targets := []string{"#1 a", "#2 b"}
	ranks := substringFilter("", targets)
	if len(ranks) != len(targets) {
		t.Errorf("empty term: got %d ranks, want %d", len(ranks), len(targets))
	}
}
