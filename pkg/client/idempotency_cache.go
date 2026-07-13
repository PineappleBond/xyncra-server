package client

import (
	"container/list"
	"sync"
)

// IdempotencyCache is a bounded LRU cache of processed idempotency keys.
// It prevents duplicate execution of replayed server-initiated requests.
// Thread-safe via sync.Mutex.
type IdempotencyCache struct {
	mu       sync.Mutex
	capacity int
	items    map[string]*list.Element
	order    *list.List // front = most recently used
}

// NewIdempotencyCache creates a new cache with the given capacity.
func NewIdempotencyCache(capacity int) *IdempotencyCache {
	return &IdempotencyCache{
		capacity: capacity,
		items:    make(map[string]*list.Element),
		order:    list.New(),
	}
}

// Contains checks if the key exists in the cache. If found, promotes to MRU.
func (c *IdempotencyCache) Contains(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	elem, ok := c.items[key]
	if !ok {
		return false
	}
	c.order.MoveToFront(elem)
	return true
}

// Put inserts or promotes a key. Evicts LRU if at capacity.
func (c *IdempotencyCache) Put(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if elem, ok := c.items[key]; ok {
		c.order.MoveToFront(elem)
		return
	}

	if c.order.Len() >= c.capacity {
		back := c.order.Back()
		if back != nil {
			c.order.Remove(back)
			delete(c.items, back.Value.(string))
		}
	}

	elem := c.order.PushFront(key)
	c.items[key] = elem
}

// Len returns the number of entries in the cache.
func (c *IdempotencyCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
