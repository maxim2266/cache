package cache

import (
	"fmt"
	"sync"
	"time"

	"golang.org/x/exp/constraints"
)

const maxCacheSize = 64 * 1024 * 1024 // arbitrary large number

// Cache is an opaque type representing a cache with keys of type "K" and values of type "V".
type Cache[K constraints.Ordered, V any] struct {
	mu   sync.Mutex
	data map[K]*cacheNode[K, V]
	lru  *cacheNode[K, V]

	size    int
	ttl     time.Duration
	backend func(K) (V, error)
}

type cacheNode[K constraints.Ordered, V any] struct {
	prev, next *cacheNode[K, V]
	once       sync.Once

	key   K
	value V
	err   error
	ts    time.Time
}

// New creates a new Cache with keys of type "K" and values of type "V".
func New[K constraints.Ordered, V any](size int, ttl time.Duration, backend func(K) (V, error)) *Cache[K, V] {
	if size < 2 || size > maxCacheSize {
		fail[K, V]("invalid capacity of %d items", size)
	}

	if backend == nil {
		fail[K, V]("nil backend function")
	}

	return &Cache[K, V]{
		data:    make(map[K]*cacheNode[K, V], size),
		size:    size,
		ttl:     ttl,
		backend: backend,
	}
}

//go:noinline
func fail[K, V any](msg string, args ...any) {
	var k K
	var v V

	prefix := fmt.Sprintf("attempted to create a Cache[%T,%T] with ", k, v)

	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}

	panic(prefix + msg)
}

// Get retrieves the value associated with the given key, invoking backend where necessary.
func (c *Cache[K, V]) Get(key K) (V, error) {
	node := c.get(key)

	node.once.Do(func() {
		defer func() {
			if p := recover(); p != nil {
				node.err = fmt.Errorf("panic: %+v", p)
				panic(p)
			}
		}()

		node.value, node.err = c.backend(node.key)
	})

	return node.value, node.err
}

// Delete evicts the given key from the cache.
func (c *Cache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if node := c.data[key]; node != nil {
		c.lruRemove(node)
		node.next, node.prev = nil, nil // help gc
		delete(c.data, key)
	}
}

func (c *Cache[K, V]) get(key K) (node *cacheNode[K, V]) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if node = c.data[key]; node != nil { // found
		if time.Since(node.ts) > c.ttl {
			c.lruRemove(node)
			node.next, node.prev = nil, nil // help gc
			node = c.newNode(node.key)
		} else if node == c.lru.next { // most recent
			return
		} else {
			c.lruRemove(node)
		}
	} else { // not found
		if len(c.data) == c.size { // cache full
			// delete the least recent
			node = c.lru
			c.lru = node.prev
			node.prev.next, node.next.prev = node.next, node.prev
			node.next, node.prev = nil, nil // help gc
			delete(c.data, node.key)
		}

		node = c.newNode(key)
	}

	// add the node as the most recent
	if c.lru == nil {
		c.lru = node
		node.next, node.prev = node, node
	} else {
		node.next, node.prev = c.lru.next, c.lru
		node.next.prev, node.prev.next = node, node
	}

	return
}

func (c *Cache[K, V]) newNode(key K) (node *cacheNode[K, V]) {
	node = &cacheNode[K, V]{
		key: key,
		ts:  time.Now(),
	}

	c.data[key] = node
	return
}

func (c *Cache[K, V]) lruRemove(node *cacheNode[K, V]) {
	if node.next == node {
		c.lru = nil
	} else {
		if c.lru == node {
			c.lru = node.prev
		}

		node.prev.next, node.next.prev = node.next, node.prev
	}
}
