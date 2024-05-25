package cache

import (
	"context"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOneRecord(t *testing.T) {
	var backend tracingBackend

	c := New(5, time.Hour, backend.fn)

	if err := assertEmpty(c); err != nil {
		t.Error("new cache is not empty:", err)
		return
	}

	// add one valid key
	v, err := c.Get(5)

	if err != nil {
		t.Error("error inserting a key:", err)
		return
	}

	if v != -5 {
		t.Errorf("unexpected value: %d instead of -5", v)
	}

	if err = checkState(c, []int{5}, validKey); err != nil {
		t.Error("error after the first insert:", err)
		return
	}

	// delete the key
	c.Delete(5)

	if err = assertEmpty(c); err != nil {
		t.Error("error after deleting a key:", err)
		return
	}

	// try the same, but with invalid key
	_, err = c.Get(1000)

	if err == nil {
		t.Error("missing error while inserting an invalid key")
		return
	}

	if err = checkState(c, []int{1000}, validKey); err != nil {
		t.Error("error after the first insert:", err)
		return
	}

	// delete the key
	c.Delete(1000)

	if err = assertEmpty(c); err != nil {
		t.Error("error after deleting a key:", err)
		return
	}

	// check trace
	if err = matchTraces(backend.trace, []int{5, 1000}); err != nil {
		t.Error("trace mismatch:", err)
		return
	}
}

func TestFewRecords(t *testing.T) {
	var (
		backend tracingBackend
		err     error
	)

	c := New(2, time.Hour, backend.fn)

	if err = assertEmpty(c); err != nil {
		t.Error("new cache is not empty:", err)
		return
	}

	if err = fill(c.Get, []int{1, 2, 3}, validKey); err != nil {
		t.Error("error filling the cache:", err)
		return
	}

	if err = checkState(c, []int{2, 3}, validKey); err != nil {
		t.Error("invalid state after fill:", err)
		return
	}

	if err = matchTraces(backend.trace, []int{1, 2, 3}); err != nil {
		t.Error("trace mismatch:", err)
		return
	}
}

func TestCacheOperation(t *testing.T) {
	rand.Seed(time.Now().UnixNano())

	const cacheSize = 5

	var (
		backend tracingBackend
		err     error
	)

	c := New(cacheSize, time.Hour, backend.fn)

	if err = fill(c.Get, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}, validKey); err != nil {
		t.Error("error filling the cache:", err)
		return
	}

	// LRU: {5, 6, 7, 8, 9}
	if err = checkState(c, []int{5, 6, 7, 8, 9}, validKey); err != nil {
		t.Error("invalid cache state:", err)
		return
	}

	if err = fill(c.Get, []int{6, 7}, validKey); err != nil {
		t.Error("error filling the cache:", err)
		return
	}

	// LRU: {5, 8, 9, 6, 7}
	if err = checkState(c, []int{5, 8, 9, 6, 7}, validKey); err != nil {
		t.Error("invalid cache state:", err)
		return
	}

	if err = fill(c.Get, []int{42, 9}, validKey); err != nil {
		t.Error("error filling the cache:", err)
		return
	}

	// LRU: {8, 6, 7, 42, 9}
	if err = checkState(c, []int{8, 6, 7, 42, 9}, validKey); err != nil {
		t.Error("invalid cache state:", err)
		return
	}

	c.Delete(6)
	c.Delete(8)
	c.Delete(9)

	// LRU: {7, 42}
	if err = checkState(c, []int{7, 42}, validKey); err != nil {
		t.Error("invalid cache state:", err)
		return
	}

	// check traces
	if err = matchTraces(backend.trace, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 42}); err != nil {
		t.Error("invalid trace:", err)
		t.Log(backend.trace)
		return
	}
}

func TestRandomFill(t *testing.T) {
	var (
		backend tracingBackend
		calls   int
	)

	const cacheSize = 90

	c := New(cacheSize, time.Hour, backend.fn)
	get := func(k int) (int, error) {
		calls++
		return c.Get(k)
	}

	var keys [500000]int

	for i := range keys {
		keys[i] = rand.Intn(100)
	}

	if err := fill(get, keys[:], validKey); err != nil {
		t.Error("error filling the cache:", err)
		return
	}

	// calculate cache efficiency
	ratio := 100 * float64(calls-len(backend.trace)) / float64(calls)

	t.Logf("cache efficiency %.2f%%", ratio)

	exp := float64(cacheSize)

	if math.Abs((ratio-exp)/exp) > 0.01 {
		t.Errorf("cache efficiency: %.2f%% instead of %.2f%%", ratio, exp)
		return
	}
}

func TestCacheMiss(t *testing.T) {
	const N = 1000

	var trace []int

	backend := func(k int) (int, error) {
		trace = append(trace, k)
		return -k, nil
	}

	c := New(100, time.Hour, backend)

	for i := 0; i < N; i++ {
		getOne(c, i)
	}

	if len(trace) != N {
		t.Errorf("unexpected number of backend calls: %d intead of %d", len(trace), N)
		return
	}

	for i := 0; i < N; i++ {
		if trace[i] != i {
			t.Errorf("unexpected key in backend trace: %d instead of %d", trace[i], i)
			return
		}
	}
}

func TestConcurrentAccess(t *testing.T) {
	const (
		threads   = 1
		cacheSize = 90
	)

	var (
		backend intBackendMT
		wg      sync.WaitGroup
		calls   uint64
	)

	c := New(cacheSize, 500*time.Microsecond, backend.fn)

	get := func(k int) (int, error) {
		atomic.AddUint64(&calls, 1)
		return c.Get(k)
	}

	wg.Add(threads)

	for i := 0; i < threads; i++ {
		go func() {
			defer wg.Done()

			var keys [100000]int

			for i := range keys {
				keys[i] = rand.Intn(100)
			}

			ts := time.Now()

			for time.Since(ts) < time.Second {
				for _, k := range keys {
					v, err := get(k)

					if validKey(k) {
						if err != nil {
							t.Error("unexpected error:", err)
							return
						}

						if v != -k {
							t.Errorf("value mismatch for key %d: %d instead of %d", k, v, -k)
							return
						}
					} else if err == nil {
						t.Errorf("missing error for key %d", k)
						return
					}
				}
			}
		}()
	}

	wg.Wait()

	ratio := 100 * float64(calls-(backend.hit+backend.miss)) / float64(calls)
	t.Logf("cache efficiency %.2f%%", ratio)

	exp := float64(cacheSize)

	if math.Abs((ratio-exp)/exp) > 0.01 {
		t.Errorf("cache efficiency: %.2f%% instead of %.2f%%", ratio, exp)
		return
	}
}

// benchmarks ---------------------------------------------------------------------------
func BenchmarkCache(b *testing.B) {
	const cacheSize = 100

	c := New(cacheSize, time.Hour, simpleBackend)

	// warm-up
	for k := 0; k < cacheSize; k++ {
		if err := getOne(c, k); err != nil {
			b.Error(err)
			return
		}
	}

	// run
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if err := getOne(c, i%cacheSize); err != nil {
			b.Error(err)
			return
		}
	}
}

const benchCacheSize = 1000

func BenchmarkContended_1(b *testing.B) {
	bench(b, benchCacheSize, 1)
}

func BenchmarkContended_10(b *testing.B) {
	bench(b, benchCacheSize, 10)
}

func BenchmarkContended_100(b *testing.B) {
	bench(b, benchCacheSize, 100)
}

func BenchmarkContended_1000(b *testing.B) {
	bench(b, benchCacheSize, 1000)
}

func BenchmarkContended_10000(b *testing.B) {
	bench(b, benchCacheSize, 10000)
}

func bench(b *testing.B, cacheSize, numBgReaders int) {
	atomic.StoreUint32(&numBackendCalls, 0)

	c := New(cacheSize, time.Hour, benchBackend)

	// warm-up
	for k := 0; k < cacheSize; k++ {
		if err := getOne(c, k); err != nil {
			b.Error(err)
			return
		}
	}

	// start background readers
	ctx, cancel := context.WithCancel(context.Background())

	var wg sync.WaitGroup

	wg.Add(numBgReaders)

	for i := 0; i < numBgReaders; i++ {
		go func() {
			defer wg.Done()

			for {
				select {
				case <-ctx.Done():
					return
				default:
					for i := 0; i < cacheSize; i++ {
						if err := getOne(c, i%cacheSize); err != nil {
							b.Error(err)
							cancel()
							return
						}
					}
				}
			}
		}()
	}

	// run
	func() {
		defer cancel()

		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			if err := getOne(c, i%cacheSize); err != nil {
				b.Error(err)
				return
			}
		}

		b.StopTimer()
	}()

	wg.Wait()

	if nc := atomic.LoadUint32(&numBackendCalls); nc != benchCacheSize {
		b.Errorf("unexpected number of backend calls: %d instead of %d", nc, benchCacheSize)
	}
}

// backend for benchmark
var numBackendCalls = uint32(0)

func benchBackend(key int) (int, error) {
	atomic.AddUint32(&numBackendCalls, 1)

	if key >= 0 && key < benchCacheSize {
		return -key, nil
	}

	return 0, fmt.Errorf("key not found: %d", key)
}
