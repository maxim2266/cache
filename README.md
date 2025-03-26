# Generic LRU cache for Go

[![License: BSD 3 Clause](https://img.shields.io/badge/License-BSD_3--Clause-yellow.svg)](https://opensource.org/licenses/BSD-3-Clause)
[![GoDoc](https://godoc.org/github.com/maxim2266/cache?status.svg)](https://godoc.org/github.com/maxim2266/cache)

Probably, a 121<sup>st</sup> implementation of cache since Go generics were introduced, but this one actually
solves the race condition
[problem](https://old.reddit.com/r/golang/comments/lw9ujj/ristretto_the_most_performant_concurrent_cache/gpgxnx9/)
that exists in some other more starred packages.

### API

An LRU cache for any given types `K` and `V` ("key" and "value", respectively) can be constructed
using function<br/>
```Go
func New(size int, ttl time.Duration, backend func(K) (V, error)) *Cache[K,V]
```
Parameters:
* Maximum size of the cache (a positive integer);
* Time-to-live for cache elements (can be set to something like ten years to retain "forever");
* Backend function to call when a cache miss occurs. The function is expected to return a value
	for the given key, or an error. Both the value _and_ the error are stored in the cache.
	A slow backend function is not going to block access to the entire cache, only to the
	corresponding value.

The constructor returns a pointer to a newly created cache object.

A cache object has two public methods:
* `Get(K) (V, error)`: given a key, it returns the corresponding value, or an error. On cache miss
the result is transparently retrieved from the backend. The cache itself does not produce any error,
so all the errors are from the backend only. Notably, this method has the same signature as the
backend function, and it may be considered as a wrapper around the backend that adds
[memoisation](https://en.wikipedia.org/wiki/Memoization). For example, given the backend function
	```Go
	func getUserInfo(userID int) (*UserInfo, error)
	```
	a caching wrapper with the same signature can be created like
	```Go
	getUserInfoCached := cache.New(1000, 2 * time.Hour, getUserInfo).Get
	```
	(assuming in this particular scenario there is no need to ever delete a record from the cache).
* `Delete(K)`: deletes the specified key from the cache; no-op if the key is not present.

The cache object is safe for concurrent access, but the backend function may be called from multiple threads.
To flush the cache simply replace it with a newly created one.

### Benchmarks
```
▶ go version
go version go1.24.1 linux/amd64
▶ go test -bench .
goos: linux
goarch: amd64
pkg: github.com/maxim2266/cache
cpu: Intel(R) Core(TM) i5-8500T CPU @ 2.10GHz
BenchmarkCache-6                20900569                56.08 ns/op
BenchmarkContended_1-6           7087633               186.1 ns/op
BenchmarkContended_10-6           656268              2455 ns/op
BenchmarkContended_100-6           32578             35045 ns/op
BenchmarkContended_1000-6           3614            434145 ns/op
BenchmarkContended_10000-6           328           6779290 ns/op
PASS
```
Here the first benchmark demonstrates uncontended single-threaded performance by reading the
cache from a single goroutine. All the remaining benchmarks access the cache in parallel with
other 1 to 10000 goroutines reading the cache in a busy loop, running on 6 real CPU cores.
The cache is instantiated with integer keys and values.
