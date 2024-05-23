package cache

import (
	"errors"
	"strconv"
	"sync"
	"time"
	"unsafe"
)

const maxCacheSize = 64 * 1024 * 1024 // arbitrary large number

// LRU is an opaque type representing an LRU cache with keys of type "K" and values of type "V".
type LRU[K comparable, V any] struct {
	mu    sync.Mutex           // mutex to protect the cache
	nodes map[K]*lruNode[K, V] // mapping from keys to nodes
	list  listNode             // LRU list

	size    int                // max. number of items in the cache
	ttl     time.Duration      // time-to-live for each item
	backend func(K) (V, error) // function for fetching data on cache miss
}

// New creates a new LRU cache with keys of type "K" and values of type "V".
func New[K comparable, V any](
	size int,
	ttl time.Duration,
	backend func(K) (V, error),
) (c *LRU[K, V]) {
	// parameter validation
	if size < 2 || size > maxCacheSize {
		panic("attempt to create an LRU cache with invalid capacity of " +
			strconv.Itoa(size) + " items")
	}

	switch {
	case ttl < 0:
		panic("attempt to create an LRU cache with negative TTL")
	case ttl == 0:
		// keep "forever"
		ttl = 50 * 365 * 24 * time.Hour
	}

	if backend == nil {
		panic("attempt to create an LRU cache with nil backend function")
	}

	// new cache
	c = &LRU[K, V]{
		nodes:   make(map[K]*lruNode[K, V], size),
		size:    size,
		ttl:     ttl,
		backend: backend,
	}

	// prime the LRU list
	c.list.next, c.list.prev = &c.list, &c.list

	return
}

// Delete evicts the given key from the cache.
func (c *LRU[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if node := c.nodes[key]; node != nil {
		delete(c.nodes, key)
		node.purge()
	}
}

// Get retrieves the value associated with the given key, invoking backend where necessary.
func (c *LRU[K, V]) Get(key K) (V, error) {
	node := c.get(key)

	node.once.Do(func() {
		defer func() {
			if p := recover(); p != nil {
				node.err = errors.New("backend function panicked")
				panic(p)
			}
		}()

		node.value, node.err = c.backend(node.key)
	})

	return node.value, node.err
}

// get or add a cache node
func (c *LRU[K, V]) get(key K) (node *lruNode[K, V]) {
	c.mu.Lock()
	defer c.mu.Unlock()

	node = c.nodes[key]

	switch {
	case node != nil: // cache hit
		if time.Since(node.ts) < c.ttl { // happy path
			node.mtf(&c.list)
			return
		}

		// purge the expired node (no need to delete the key)
		node.purge()

	case len(c.nodes) >= c.size: // cache full
		// delete the least recent
		node = (*lruNode[K, V])(unsafe.Pointer(c.list.prev))

		delete(c.nodes, node.key)
		node.purge()
	}

	// allocate and add a new node as the most recent
	node = &lruNode[K, V]{key: key, ts: time.Now()}

	node.addTo(&c.list)
	c.nodes[key] = node

	return
}

// cache node
type lruNode[K comparable, V any] struct {
	listNode

	once sync.Once // for locking the node while fetching data

	key   K         // key
	value V         // value
	err   error     // error
	ts    time.Time // timestamp
}

// LRU list
type listNode struct {
	next, prev *listNode
}

// add the node to the given root as the most recent item
func (l *listNode) addTo(root *listNode) {
	l.next = root.next
	l.prev = root
	l.prev.next = l
	l.next.prev = l
}

// remove the node from the list
func (l *listNode) remove() {
	l.prev.next = l.next
	l.next.prev = l.prev
}

// purge the node from the list (remove and set pointers to nil for gc)
func (l *listNode) purge() {
	l.remove()
	l.next, l.prev = nil, nil // help gc
}

// move the node to the top of the list at root (MTF)
func (l *listNode) mtf(root *listNode) {
	if l != root.next {
		l.remove()
		l.addTo(root)
	}
}
