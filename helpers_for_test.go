package cache

import (
	"errors"
	"fmt"
	"sync/atomic"
	"unsafe"
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
		return fmt.Errorf("trace length mismatch: %v instead of %v", got, exp)
	}

	for i, v := range got {
		if v != exp[i] {
			return fmt.Errorf("trace mismatch @ %d: %d instead of %d", i, v, exp[i])
		}
	}

	return nil
}

// get one valid record
func getOne(c *LRU[int, int], k int) error {
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

// check if the cache is empty
func assertEmpty(c *LRU[int, int]) error {
	if len(c.nodes) != 0 {
		return fmt.Errorf("unexpected cache map size: %d", len(c.nodes))
	}

	if c.list.next != c.list.prev || c.list.next != &c.list {
		return errors.New("non-empty LRU list")
	}

	return nil
}

// validate cache content by inspecting its internals; in LRU order
func checkState(c *LRU[int, int], keys []int, valid func(int) bool) error {
	// initial checks
	if len(c.nodes) != len(keys) {
		return fmt.Errorf("unexpected size of cache map: %d instead of %d",
			len(c.nodes), len(keys))
	}

	if len(keys) == 0 {
		return nil
	}

	// fetch nodes
	nodes, err := lruNodeList(c)

	if err != nil {
		return err
	}

	if len(nodes) != len(keys) {
		return fmt.Errorf("unexpected number of nodes: %d instead of %d", len(nodes), len(keys))
	}

	// validate content
	for i, k := range keys {
		node, found := c.nodes[k]

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

		if node != nodes[i] {
			return fmt.Errorf("node mismatch at index %d", i)
		}
	}

	return nil
}

func lruNodeList(c *LRU[int, int]) ([]*lruNode[int, int], error) {
	res := make([]*lruNode[int, int], 0, len(c.nodes))

	// collect nodes, starting from least recent
	for p := c.list.prev; p != &c.list; p = p.prev {
		res = append(res, (*lruNode[int, int])(unsafe.Pointer(p)))
	}

	// validate via reverse traversal
	i := len(res)

	for p := c.list.next; p != &c.list; p = p.next {
		node := (*lruNode[int, int])(unsafe.Pointer(p))

		if i--; i < 0 {
			return nil, fmt.Errorf("unexpected node with key %d and value %d", node.key, node.value)
		}

		if node.key != res[i].key {
			return nil, fmt.Errorf("unexpected node key: %d instead of %d", node.key, res[i].key)
		}
	}

	if i > 0 {
		return nil, fmt.Errorf("missing %d nodes", i)
	}

	return res, nil
}
