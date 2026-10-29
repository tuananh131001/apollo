package deezer

import (
	"container/list"
	"sync"
	"time"
)

// coverCache remembers FindCover answers, including that Deezer has no cover,
// so repeated searches do not spend Deezer's rate limit. It holds at most size
// entries and forgets the least recently used one first.
type coverCache struct {
	size int

	mu      sync.Mutex
	order   *list.List // of *coverEntry, most recently used first
	entries map[string]*list.Element
}

type coverEntry struct {
	key     string
	cover   string
	expires time.Time
}

func newCoverCache(size int) *coverCache {
	return &coverCache{size: size, order: list.New(), entries: map[string]*list.Element{}}
}

// get returns the cover cached for key, and false when there is none or it
// has expired.
func (c *coverCache) get(key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		return "", false
	}
	e := el.Value.(*coverEntry)
	if time.Now().After(e.expires) {
		c.order.Remove(el)
		delete(c.entries, key)
		return "", false
	}
	c.order.MoveToFront(el)
	return e.cover, true
}

// put caches cover, which may be empty for no cover, under key for ttl.
func (c *coverCache) put(key, cover string, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := &coverEntry{key: key, cover: cover, expires: time.Now().Add(ttl)}
	if el, ok := c.entries[key]; ok {
		el.Value = e
		c.order.MoveToFront(el)
		return
	}
	c.entries[key] = c.order.PushFront(e)
	if c.order.Len() > c.size {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.entries, oldest.Value.(*coverEntry).key)
	}
}
