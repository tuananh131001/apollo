// Package cache holds in-memory caches shared by apollo's services.
package cache

import (
	"container/list"
	"time"
)

// NewLRU returns an LRU that holds at most size entries.
func NewLRU[V any](size int) *LRU[V] {
	return &LRU[V]{size: size, order: list.New(), entries: map[string]*list.Element{}}
}

// Get returns the value cached for key, and false when there is none or it
// has expired.
func (c *LRU[V]) Get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[key]
	if !ok {
		var zero V
		return zero, false
	}
	e := el.Value.(*entry[V])
	if time.Now().After(e.expires) {
		c.order.Remove(el)
		delete(c.entries, key)
		var zero V
		return zero, false
	}
	c.order.MoveToFront(el)
	return e.value, true
}

// Put caches value under key for ttl.
func (c *LRU[V]) Put(key string, value V, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := &entry[V]{key: key, value: value, expires: time.Now().Add(ttl)}
	if el, ok := c.entries[key]; ok {
		el.Value = e
		c.order.MoveToFront(el)
		return
	}
	c.entries[key] = c.order.PushFront(e)
	if c.order.Len() > c.size {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.entries, oldest.Value.(*entry[V]).key)
	}
}

// Delete forgets key.
func (c *LRU[V]) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[key]; ok {
		c.order.Remove(el)
		delete(c.entries, key)
	}
}
