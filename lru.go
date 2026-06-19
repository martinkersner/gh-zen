package main

import "container/list"

// lruCache is a small, bounded, in-memory cache with least-recently-used
// eviction. It exists so the detail body/diff caches (see model.bodyCache /
// model.diffCache) can't grow without bound over a long session that pages
// through many items: each cache is capped at a fixed number of entries and the
// least-recently-used entry is evicted once the cap is exceeded.
//
// Both reads (get) and writes (set) mark an entry as most-recently-used, so an
// entry that is actively being read or merged (e.g. the prefetch -> full-fetch
// label merge in the bodyMsg/labelsMsg handlers) is never evicted out from under
// an in-flight update. It is NOT safe for concurrent use; all access happens on
// the single Bubble Tea update goroutine.
type lruCache[V any] struct {
	cap   int
	ll    *list.List               // front = most-recently-used, back = LRU
	items map[string]*list.Element // key -> element holding an *lruEntry[V]
}

// lruEntry is the value stored in each list element: the key (so eviction can
// delete the matching map entry from the back of the list) and its value.
type lruEntry[V any] struct {
	key   string
	value V
}

// newLRUCache returns an empty cache bounded to capacity entries. A capacity of
// zero or less is treated as 1 so a degenerate config can never disable caching
// entirely (which would defeat the point of the cache).
func newLRUCache[V any](capacity int) *lruCache[V] {
	if capacity < 1 {
		capacity = 1
	}
	return &lruCache[V]{
		cap:   capacity,
		ll:    list.New(),
		items: make(map[string]*list.Element, capacity),
	}
}

// get returns the value for key and whether it was present, marking it as
// most-recently-used on a hit so an actively-read entry resists eviction.
func (c *lruCache[V]) get(key string) (V, bool) {
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		return el.Value.(*lruEntry[V]).value, true
	}
	var zero V
	return zero, false
}

// set stores value under key, marking it most-recently-used. Updating an
// existing key replaces its value in place (no growth). Inserting a new key past
// the capacity evicts the least-recently-used entry.
func (c *lruCache[V]) set(key string, value V) {
	if el, ok := c.items[key]; ok {
		el.Value.(*lruEntry[V]).value = value
		c.ll.MoveToFront(el)
		return
	}
	c.items[key] = c.ll.PushFront(&lruEntry[V]{key: key, value: value})
	if c.ll.Len() > c.cap {
		c.evictOldest()
	}
}

// has reports whether key is present WITHOUT updating recency, mirroring a plain
// map membership check (e.g. the "don't clobber / already cached" guards). Use
// get when the value is needed or the entry should be kept fresh.
func (c *lruCache[V]) has(key string) bool {
	_, ok := c.items[key]
	return ok
}

// len reports the current number of cached entries (for tests/assertions).
func (c *lruCache[V]) len() int {
	return c.ll.Len()
}

// evictOldest removes the least-recently-used entry (the back of the list).
func (c *lruCache[V]) evictOldest() {
	el := c.ll.Back()
	if el == nil {
		return
	}
	c.ll.Remove(el)
	delete(c.items, el.Value.(*lruEntry[V]).key)
}
