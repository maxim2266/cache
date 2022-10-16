# Generic LRU cache for Go

[![License: BSD 3 Clause](https://img.shields.io/badge/License-BSD_3--Clause-yellow.svg)](https://opensource.org/licenses/BSD-3-Clause)
[![GoDoc](https://godoc.org/github.com/maxim2266/cache?status.svg)](https://godoc.org/github.com/maxim2266/cache)

Probably, a 121<sup>st</sup> implementation of cache since Go generics were introduced, but this one actually
solves the race condition
[problem](https://old.reddit.com/r/golang/comments/lw9ujj/ristretto_the_most_performant_concurrent_cache/gpgxnx9/)
that exists in some other more starred packages.

### API

A cache for any given types `K` and `V` ("key" and "value", respectively) can be constructed
using function<br/>
```Go
func New(size int, ttl time.Duration, backend func(K) (V, error)) *Cache[K,V]
```
Parameters:
* Maximum size of the cache (a positive integer);
* Time-to-live for cache elements (can be set to something like ten years to retain "forever");
* Back-end function to call when a cache miss occurs. The function is expected to return a value
	for the given key, or an error. Both the value _and_ the error are stored in the cache.
	A slow back-end function is not going to block access to the entire cache, only to the
	corresponding value.

The constructor returns a pointer to a newly created cache object.

A cache object has two public methods:
* `Get(K) (V, error)`: given a key, it returns the corresponding value, or an error. On cache miss
the result is transparently retrieved from the back-end. The cache itself does not produce any error,
so all the errors are from the back-end. Notably, this method has the same signature as the
back-end function, and it may be considered as a wrapper around the back-end that adds
[memoisation](https://en.wikipedia.org/wiki/Memoization). For example, given the back-end function
	```Go
	func getUserInfo(userID int) (*UserInfo, error)
	```
	a caching wrapper with the same signature can be created like
	```Go
	getUserInfoCached := cache.New(1000, 2 * time.Hour, getUserInfo).Get
	```
	(assuming in this particular scenario there is no need to ever delete a record from the cache).
* `Delete(K)`: deletes the specified key from the cache.

The cache object is safe for concurrent access. To flush the cache simply replace it with a
newly created one.

### Benchmarks
```
▶ go version
go version go1.19.2 linux/amd64
▶ go test -bench .
goos: linux
goarch: amd64
pkg: github.com/maxim2266/cache
cpu: Intel(R) Core(TM) i5-8500T CPU @ 2.10GHz
BenchmarkCache-6            	18281998	        63.50 ns/op
BenchmarkContendedCache-6   	  691878	      1624 ns/op
```
Here the first benchmark reads the cache from a single goroutine, while the second one is the same
benchmark run in parallel with another 10 goroutines accessing the cache concurrently. The cache
is instantiated with integer keys and values.
