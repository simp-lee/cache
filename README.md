# Sharded In-Memory Cache

This is a version of the sharded in-memory cache implementation with significant improvements in performance and features.

## Important Notice
This is a new version of [SwiftCache](https://github.com/simp-lee/SwiftCache). Both versions use sharded architecture for high concurrent performance, but with different focus:

### Key Improvements in this version
- **Persistence Support**: Added ability to persist cache data to disk and reload on startup
- **Enhanced Interface**: Added utility functions like `GetOrSet`, `GetOrSetFunc`, `GetWithExpiration`
- **Comprehensive Statistics**: Added detailed cache statistics including hit rates and item counts
- **Better Memory Management**: More efficient memory usage for large datasets
- **Eviction Callbacks**: Support for custom eviction handlers

### Note on Eviction Policies
This version focuses on time-based expiration. If you need LRU/FIFO eviction policies, please use [SwiftCache](https://github.com/simp-lee/SwiftCache).

## Performance

Both versions are designed for high efficiency and performance in concurrent environments. The choice between them should be based on feature requirements rather than performance differences.

## Performance Comparison

Benchmark results comparing our ShardedCache with [go-cache](https://github.com/patrickmn/go-cache) and sync.Map:

### Get Operations
```
ShardedCache/Get    22,028,978 ops/s    55.08 ns/op    100% hit    23 B/op    1 allocs/op
GoCache/Get         16,556,176 ops/s    76.63 ns/op    100% hit    23 B/op    1 allocs/op
SyncMap/Get         22,973,101 ops/s    54.19 ns/op    100% hit    23 B/op    1 allocs/op
```

### Set Operations
```
ShardedCache/Set    16,764,108 ops/s    92.24 ns/op    75 B/op    5 allocs/op
GoCache/Set          3,599,025 ops/s   371.20 ns/op    63 B/op    4 allocs/op
```

### Mixed Operations (Get/Set)
```
ShardedCache/Mixed  18,238,190 ops/s    73.12 ns/op    100% hit    31 B/op    2 allocs/op
GoCache/Mixed        2,089,064 ops/s   580.70 ns/op    100% hit    31 B/op    2 allocs/op
```

### Memory Usage
Our ShardedCache demonstrates efficient memory management while maintaining high performance:
- Can handle over 2 million items with approximately 535MB memory usage
- Efficient memory allocation with minimal garbage collection impact

Key Performance Advantages over go-cache:
- 27% faster Get operations
- 4x faster Set operations
- 8x faster Mixed operations
- Better concurrent performance through sharding
- More efficient memory management for large datasets

## Installation

To use this cache in your Go project, you can import it as follows:

```go
go get github.com/simp-lee/cache
```

## Usage

### Interface

The cache implements the following interface:

```go
type CacheInterface interface {
    // Set sets a value in the cache
    Set(key string, value interface{})
    
    // SetWithExpiration sets a value in the cache with an expiration time
    SetWithExpiration(key string, value interface{}, expiration time.Duration)
    
    // Get retrieves a value from the cache
    Get(key string) (interface{}, bool)
    
    // GetWithExpiration retrieves a value and its expiration time from the cache
    GetWithExpiration(key string) (interface{}, *time.Time, bool)
    
    // Delete removes a value from the cache
    Delete(key string) error
    
    // GetOrSet returns existing value or sets and returns new value
    GetOrSet(key string, value interface{}) interface{}
    
    // GetOrSetFunc returns existing value or sets and returns computed value
    GetOrSetFunc(key string, f func() interface{}) interface{}
    
    // GetOrSetFuncWithExpiration same as GetOrSetFunc but with expiration
    GetOrSetFuncWithExpiration(key string, f func() interface{}, expiration time.Duration) interface{}
    
    // Stats returns cache statistics
    Stats() map[string]interface{}
    
    // OnEvicted sets callback function to be called when an item is evicted
    OnEvicted(f func(key string, value interface{}))
    
    // Keys returns all keys in the cache
    Keys() []string
    
    // Count returns the number of items in the cache
    Count() int
    
    // Has checks if a key exists in the cache
    Has(key string) bool
    
    // Clear removes all items from the cache
    Clear() error
    
    // Close shuts down the cache and persists data if configured
    Close()
}
```

### Usage Patterns

#### 1. Singleton Pattern

```go
package main

import (
    "fmt"
    "time"
    "github.com/simp-lee/cache"
)

func main() {
    // Initialize the cache (do this once at startup)
    cache.Init(cache.Options{
        MaxSize:           1000,
        DefaultExpiration: time.Minute * 5,
        ShardCount:        32,
    })

    // Get the cache instance anywhere in your code
    c := cache.Get()

    // Use the cache
    c.Set("key", "value")
    value, exists := c.Get("key")
    if !exists {
        fmt.Println("key not found")
    }
    fmt.Println(value)
}
```

#### 2. New Instance

```go
// Create a new cache instance
c := cache.NewCache(cache.Options{
    MaxSize:           1000,
    DefaultExpiration: time.Minute * 5,
    ShardCount:        64,
})

// Use the cache
c.Set("key", "value")
value, exists := c.Get("key")
```

### Configuration Options

```go
type Options struct {
    MaxSize           int           // Maximum number of items per shard
    DefaultExpiration time.Duration // Default expiration time
    CleanupInterval   time.Duration // Interval for cleaning up expired items
    PersistPath       string        // Path for persistence files
    PersistInterval   time.Duration // Interval for persisting data to disk
    PersistThreshold  int           // Number of modifications before triggering persistence
    ShardCount        int           // Number of shards (must be power of 2)
}
```

## Contributing

Contributions are welcome! Please feel free to submit a pull request or open an issue.

## License

This project is licensed under the MIT License.