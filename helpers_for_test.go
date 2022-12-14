package cache

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
)

// tracing backend
type tracingBackend struct {
	trace []int
}

func (b *tracingBackend) fn(key int) (int, error) {
	b.trace = append(b.trace, key)

	if validKey(key) {
		return -key, nil
	}

	return 0, fmt.Errorf("key not found: %d", key)
}

// thread-safe backend with hit/miss counters
type intBackendMT struct {
	hit, miss uint64
}

func (b *intBackendMT) fn(key int) (int, error) {
	if validKey(key) {
		atomic.AddUint64(&b.hit, 1)
		return -key, nil
	}

	atomic.AddUint64(&b.miss, 1)

	return 0, fmt.Errorf("key not found: %d", key)
}

// simple backend
func simpleBackend(key int) (int, error) {
	if validKey(key) {
		return -key, nil
	}

	return 0, fmt.Errorf("key not found: %d", key)
}

func validKey(key int) bool {
	return key >= 0 && key < 100
}

// compare execution traces
func matchTraces(got, exp []int) error {
	if len(got) != len(exp) {
		return fmt.Errorf("trace mismatch: %v instead of %v", got, exp)
	}

	for i, v := range got {
		if v != exp[i] {
			return fmt.Errorf("trace mismatch @ %d: %d instead of %d", i, v, exp[i])
		}
	}

	return nil
}

// get one valid record
func getOne(c *Cache[int, int], k int) error {
	v, err := c.Get(k)

	if err != nil {
		return fmt.Errorf("unexpected error for key %d: %w", k, err)
	}

	if v != -k {
		return fmt.Errorf("value mismatch for key %d: %d instead of %d", k, v, -k)
	}

	return nil
}

// filling a cache
func fill(fn func(int) (int, error), keys []int, valid func(int) bool) error {
	for _, k := range keys {
		v, err := fn(k)

		if valid(k) {
			if err != nil {
				return fmt.Errorf("unexpected error while getting key %d: %w", k, err)
			}

			if v != -k {
				return fmt.Errorf("unexpected value %d for key %d", v, k)
			}
		} else if err == nil {
			return fmt.Errorf("missing error for key %d", k)
		}
	}

	return nil
}

// validate cache content by inspecting its internals; in LRU order
func checkState(c *Cache[int, int], keys []int, valid func(int) bool) error {
	// initial checks
	if len(c.data) != len(keys) {
		return fmt.Errorf("unexpected size of cache map: %d instead of %d",
			len(c.data), len(keys))
	}

	if len(keys) == 0 {
		if c.lru != nil {
			return fmt.Errorf("unexpected content in LRU: key %d", c.lru.key)
		}

		return nil
	}

	p := c.lru

	// validate content
	for i, k := range keys {
		node, found := c.data[k]

		if !found {
			return fmt.Errorf("missing cache node for key %d", k)
		}

		if node == nil {
			return fmt.Errorf("nil cache node for key %d", k)
		}

		if node.key != k {
			return fmt.Errorf("unexpected key %d in node for key %d", node.key, k)
		}

		if valid(k) {
			if node.value != -k {
				return fmt.Errorf("unexpected value in node %d: %d instead of %d", k, node.value, -k)
			}
		} else if node.err == nil {
			return fmt.Errorf("missing error in node %d", k)
		}

		// check LRU node
		if p != node {
			return fmt.Errorf("LRU and cache node mismatch: LRU key %d, cache map key %d", p.key, node.key)
		}

		if p.next.prev != p || p.prev.next != p {
			return fmt.Errorf("invalid node links for key %d", k)
		}

		// move prev LRU node
		if p = p.prev; p == c.lru && i != len(keys)-1 {
			return fmt.Errorf("LRU list terminates at key %d", k)
		}
	}

	// check LRU pointer
	if p != c.lru {
		return errors.New("LRU list is longer than cache map")
	}

	return nil
}

// check if the cache is empty
func assertEmpty(c *Cache[int, int]) error {
	if c.lru != nil {
		return errors.New("non-null LRU pointer")
	}

	if len(c.data) != 0 {
		return fmt.Errorf("unexpected cache map size: %d", len(c.data))
	}

	return nil
}

// dump cache LRU list, from least to most recent
func dumpLRU(c *Cache[int, int]) string {
	if c.lru == nil {
		return "LRU: (empty)"
	}

	var res strings.Builder

	res.WriteString("LRU:")

	for node := c.lru; ; {
		fmt.Fprintf(&res,
			"\n{ key: %v, value: %v, error: %v }",
			node.key, node.value, node.err)

		if node = node.prev; node == c.lru {
			break
		}
	}

	return res.String()
}

func dumpRevLRU(c *Cache[int, int]) string {
	if c.lru == nil {
		return "LRU: (empty)"
	}

	var res strings.Builder

	res.WriteString("LRU (reverse order):")

	for node := c.lru.next; ; node = node.next {
		fmt.Fprintf(&res,
			"\n{ key: %v, value: %v, error: %v }",
			node.key, node.value, node.err)

		if node == c.lru {
			break
		}
	}

	return res.String()
}
