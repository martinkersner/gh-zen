package main

import (
	"fmt"
	"testing"
)

// keys returns the cache's current keys ordered most-recently-used first, for
// recency-order assertions.
func (c *lruCache[V]) keys() []string {
	var out []string
	for el := c.ll.Front(); el != nil; el = el.Next() {
		out = append(out, el.Value.(*lruEntry[V]).key)
	}
	return out
}

// A fresh cache is empty and a missing key reports not-found with the zero value.
func TestLRUGetMissing(t *testing.T) {
	c := newLRUCache[string](3)
	if c.len() != 0 {
		t.Fatalf("new cache len = %d, want 0", c.len())
	}
	if v, ok := c.get("nope"); ok || v != "" {
		t.Errorf("get(missing) = (%q, %v), want (\"\", false)", v, ok)
	}
}

// set then get returns the stored value; has reports membership.
func TestLRUSetGetHas(t *testing.T) {
	c := newLRUCache[string](3)
	c.set("a", "1")
	if v, ok := c.get("a"); !ok || v != "1" {
		t.Errorf("get(a) = (%q, %v), want (1, true)", v, ok)
	}
	if !c.has("a") {
		t.Error("has(a) = false, want true")
	}
	if c.has("b") {
		t.Error("has(b) = true, want false")
	}
}

// Inserting past the cap evicts the least-recently-used entry.
func TestLRUEvictsOldestPastCap(t *testing.T) {
	c := newLRUCache[int](2)
	c.set("a", 1)
	c.set("b", 2)
	c.set("c", 3) // over cap: a (LRU) evicted

	if c.len() != 2 {
		t.Fatalf("len = %d, want 2 (cap)", c.len())
	}
	if c.has("a") {
		t.Error("a should have been evicted as LRU")
	}
	if !c.has("b") || !c.has("c") {
		t.Error("b and c should remain")
	}
}

// A touch-on-read (get) protects a recently-read entry from eviction: reading
// the oldest entry makes a newer one the LRU victim instead.
func TestLRUGetTouchProtects(t *testing.T) {
	c := newLRUCache[int](2)
	c.set("a", 1)
	c.set("b", 2)
	// Read "a" so it becomes most-recently-used; "b" is now the LRU.
	if _, ok := c.get("a"); !ok {
		t.Fatal("get(a) miss")
	}
	c.set("c", 3) // over cap: b evicted, not a

	if !c.has("a") {
		t.Error("a was read just before insert; it must NOT be evicted")
	}
	if c.has("b") {
		t.Error("b was the LRU after reading a; it should be evicted")
	}
	if !c.has("c") {
		t.Error("c should remain")
	}
}

// A touch-on-write (re-set of an existing key) protects it: re-writing the oldest
// entry makes a newer one the LRU victim, and the entry count does not grow.
func TestLRUSetTouchProtectsAndUpdatesInPlace(t *testing.T) {
	c := newLRUCache[int](2)
	c.set("a", 1)
	c.set("b", 2)
	// Re-set "a" (new value): it becomes MRU and "b" the LRU. No growth.
	c.set("a", 10)
	if c.len() != 2 {
		t.Fatalf("len after in-place update = %d, want 2", c.len())
	}
	if v, _ := c.get("a"); v != 10 {
		t.Errorf("a = %d, want updated value 10", v)
	}
	c.set("c", 3) // over cap: b evicted, not a

	if !c.has("a") {
		t.Error("a was re-written just before insert; it must NOT be evicted")
	}
	if c.has("b") {
		t.Error("b was the LRU; it should be evicted")
	}
}

// has must NOT update recency, so a membership check can't accidentally save an
// entry from eviction.
func TestLRUHasDoesNotTouch(t *testing.T) {
	c := newLRUCache[int](2)
	c.set("a", 1)
	c.set("b", 2)
	if !c.has("a") { // membership check, must not promote a
		t.Fatal("has(a) = false")
	}
	c.set("c", 3) // a is still LRU, so it is evicted

	if c.has("a") {
		t.Error("has(a) wrongly protected a from eviction")
	}
}

// A capacity of zero or less is clamped to 1 so the cache is never disabled.
func TestLRUMinCapacity(t *testing.T) {
	for _, capArg := range []int{0, -5} {
		c := newLRUCache[int](capArg)
		c.set("a", 1)
		c.set("b", 2) // evicts a
		if c.len() != 1 {
			t.Errorf("cap=%d: len = %d, want clamped to 1", capArg, c.len())
		}
		if c.has("a") || !c.has("b") {
			t.Errorf("cap=%d: expected only b resident", capArg)
		}
	}
}

// Recency order is maintained across mixed reads and writes (table-style).
func TestLRURecencyOrder(t *testing.T) {
	c := newLRUCache[int](3)
	c.set("a", 1)
	c.set("b", 2)
	c.set("c", 3)
	// Order MRU->LRU: c, b, a
	c.get("a")    // promote a -> a, c, b
	c.set("d", 4) // over cap: b evicted -> d, a, c

	got := c.keys()
	want := []string{"d", "a", "c"}
	if len(got) != len(want) {
		t.Fatalf("keys = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("keys = %v, want %v", got, want)
		}
	}
}

// Eviction holds under sustained insertion well past the cap (no leak): the
// cache never exceeds its capacity and only the most recent N keys survive.
func TestLRUBoundedUnderManyInserts(t *testing.T) {
	const cap = 4
	c := newLRUCache[int](cap)
	for i := 0; i < 100; i++ {
		c.set(fmt.Sprintf("k%d", i), i)
		if c.len() > cap {
			t.Fatalf("len %d exceeded cap %d at insert %d", c.len(), cap, i)
		}
	}
	if c.len() != cap {
		t.Fatalf("final len = %d, want %d", c.len(), cap)
	}
	// Only the last `cap` keys remain.
	for i := 96; i < 100; i++ {
		if !c.has(fmt.Sprintf("k%d", i)) {
			t.Errorf("recent key k%d evicted", i)
		}
	}
	if c.has("k95") {
		t.Error("stale key k95 should have been evicted")
	}
}

// The bodyMsg-style merge path survives eviction pressure: an entry being read
// (get) just before more inserts is not evicted out from under a follow-up
// merge-and-write. Models the prefetch -> full-fetch label merge against a small
// cap.
func TestLRUMergePathSurvivesEviction(t *testing.T) {
	c := newLRUCache[bodyEntry](2)
	c.set("issue_1", bodyEntry{body: "list body"}) // prefetched partial entry

	// Fill the cache with other items so issue_1 would be the LRU if untouched.
	c.set("issue_2", bodyEntry{body: "b2"})

	// Read-merge-write issue_1 (touch-on-read keeps it fresh during the merge).
	e, ok := c.get("issue_1")
	if !ok {
		t.Fatal("issue_1 evicted before merge")
	}
	e.labels = []label{{name: "bug", color: "d73a4a"}}
	e.author = "octocat"
	c.set("issue_1", e)

	// A subsequent insert evicts the now-LRU issue_2, not the just-merged entry.
	c.set("issue_3", bodyEntry{body: "b3"})

	got, ok := c.get("issue_1")
	if !ok {
		t.Fatal("merged issue_1 was evicted; the merge would be lost")
	}
	if got.body != "list body" || got.author != "octocat" || len(got.labels) != 1 {
		t.Errorf("merged entry corrupted: %+v", got)
	}
	if c.has("issue_2") {
		t.Error("issue_2 should have been the eviction victim")
	}
}
