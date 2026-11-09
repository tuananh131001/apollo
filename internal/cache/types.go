package cache

import (
	"container/list"
	"sync"
	"time"
)

// LRU remembers values for a while under string keys. It holds a fixed number
// of entries and forgets the least recently used one first. It is safe for
// concurrent use.
type LRU[V any] struct {
	size int

	mu      sync.Mutex
	order   *list.List // of *entry[V], most recently used first
	entries map[string]*list.Element
}

type entry[V any] struct {
	key     string
	value   V
	expires time.Time
}
