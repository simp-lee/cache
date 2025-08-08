# Sharded In-Memory Cache

A high-performance sharded in-memory cache for Go, featuring group namespaces, disk persistence, and comprehensive cache operations. Designed for concurrent access with excellent performance characteristics.

## Important Notice
This is a new version of [SwiftCache](https://github.com/simp-lee/SwiftCache). Both versions use sharded architecture for high concurrent performance, but with different focus:

### Key Improvements in this version
- **Persistence Support**: Added ability to persist cache data to disk and reload on startup
- **Enhanced Interface**: Added utility functions like `GetOrSet`, `GetOrSetFunc`, `GetWithExpiration`
- **Comprehensive Statistics**: Added detailed cache statistics including hit rates and item counts
- **Better Memory Management**: More efficient memory usage for large datasets
- **Eviction Callbacks**: Support for custom eviction handlers
- **Group Support**: Efficient namespace management with full feature support for grouped cache operations
- **Type-Safe Get Operations**: Added utility functions like `GetTyped[T](cache, key)` to retrieve values with type safety using generics

### Note on Eviction Policies
This version focuses on time-based expiration. If you need LRU/FIFO eviction policies, please use [SwiftCache](https://github.com/simp-lee/SwiftCache).

## Performance

Both versions are designed for high efficiency and performance in concurrent environments. The choice between them should be based on feature requirements rather than performance differences.

## Performance Comparison

Benchmark results comparing our `ShardedCache` with [go-cache](https://github.com/patrickmn/go-cache), [bigcache](https://github.com/allegro/bigcache) and sync.Map:

### Get Operations
```
ShardedCache/Get    22,687,654 ops/s    52.25 ns/op    100% hit    23 B/op    1 allocs/op
GoCache/Get         16,722,361 ops/s    70.87 ns/op    100% hit    23 B/op    1 allocs/op
SyncMap/Get         23,469,448 ops/s    51.16 ns/op    100% hit    23 B/op    1 allocs/op
BigCache/Get        22,618,719 ops/s    55.79 ns/op    100% hit    54 B/op    3 allocs/op
```

### Set Operations
```
ShardedCache/Set    16,102,454 ops/s    85.04 ns/op    75 B/op    5 allocs/op
GoCache/Set          3,581,130 ops/s   395.70 ns/op    63 B/op    4 allocs/op
SyncMap/Set          4,562,907 ops/s   304.90 ns/op    48 B/op    3 allocs/op
BigCache/Set        30,792,758 ops/s    71.15 ns/op   186 B/op    4 allocs/op
```

### Mixed Operations (80% Get, 20% Set)
```
ShardedCache/Mixed  18,508,464 ops/s    65.68 ns/op    100% hit    31 B/op    2 allocs/op
GoCache/Mixed        2,130,778 ops/s   569.80 ns/op    100% hit    31 B/op    2 allocs/op
SyncMap/Mixed        2,105,696 ops/s   567.90 ns/op    100% hit    47 B/op    3 allocs/op
BigCache/Mixed      22,440,392 ops/s    54.70 ns/op    100% hit    53 B/op    3 allocs/op
```

### Memory Usage
Memory efficiency comparison with 1 million items:
```
ShardedCache    169.71 MB
GoCache         135.03 MB
SyncMap         102.77 MB
BigCache        337.07 MB
```

### Key Performance Characteristics

**`ShardedCache` demonstrates excellent performance characteristics:**
- Fast read operations: ~52ns per Get operation
- Efficient write operations: ~85ns per Set operation
- Excellent mixed workload performance: ~66ns per operation
- Balanced memory usage: ~170MB for 1M items
- Consistent performance under concurrent access
- Low allocation overhead: 1-5 allocations per operation

**Performance Advantages:**
- 35% faster Get operations than `GoCache`
- 4.6x faster Set operations than `GoCache`
- 8.6x faster Mixed operations than `GoCache`
- Comparable read performance with `SyncMap`
- Better write performance than `SyncMap` (3.6x faster)
- More balanced memory usage compared to `BigCache`

### Use Case Recommendations

- **High-Throughput Scenarios**: `ShardedCache` and `BigCache` both excel
- **Memory-Constrained Environments**: `SyncMap` or `GoCache`
- **Balanced Performance**: `ShardedCache` offers the best overall balance
- **Write-Heavy Workloads**: `BigCache` shows superior performance
- **Read-Heavy Workloads**: All implementations perform well, with `SyncMap` and `ShardedCache` leading slightly

Choose `ShardedCache` when you need:
- Consistent high performance across all operations
- Good memory efficiency
- Reliable concurrent access
- Rich feature set including expiration, statistics, and group support

## Installation

To use this cache in your Go project, you can import it as follows:

```shell
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
    
    // DeleteKeys deletes the specified keys in batch and returns the number of keys actually deleted
    DeleteKeys(keys []string) int
    
    // DeletePrefix deletes all keys with the given prefix and returns the number of keys actually deleted
    DeletePrefix(prefix string) int
    
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

    // Group provides a way to create a named group of items in the cache
    Group(name string) Group
}

type Group interface {
    // Basic operations
    Set(key string, value interface{})
    SetWithExpiration(key string, value interface{}, expiration time.Duration)
    Get(key string) (interface{}, bool)
    GetWithExpiration(key string) (interface{}, *time.Time, bool)
    Delete(key string) error
    
    // Convenience methods
    GetOrSet(key string, value interface{}) interface{}
    GetOrSetFunc(key string, f func() interface{}) interface{}
    GetOrSetFuncWithExpiration(key string, f func() interface{}, expiration time.Duration) interface{}
    DeleteKeys(keys []string) int
    DeletePrefix(prefix string) int
    
    // Group management
    Keys() []string
    Clear() error
    Count() int
    Has(key string) bool
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
    shardedcache.Init(shardedcache.Options{
        MaxSize:           1000,
        DefaultExpiration: time.Minute * 5,
        ShardCount:        32,
    })

    // Get the cache instance anywhere in your code
    cache := shardedcache.Get()

    // Use the cache
    cache.Set("key", "value")
    value, exists := cache.Get("key")
    if !exists {
        fmt.Println("key not found")
    }
    fmt.Println(value)
}
```

#### 2. New Instance

```go
// Create a new cache instance
cache := shardedcache.NewCache(shardedcache.Options{
    MaxSize:           1000,
    DefaultExpiration: time.Minute * 5,
    ShardCount:        64,
})

// Use the cache
cache.Set("key", "value")
value, exists := cache.Get("key")
```

#### 3. Group Support

Cache groups provide a way to namespace your cache keys and perform operations on a group of related items. Groups are virtual and add no memory overhead while providing full isolation between different namespaces.

```go
// Create groups for different types of items
users := cache.Group("users")
posts := cache.Group("posts")

// Set values in different groups
users.Set("1", userData)
posts.Set("1", postData)

// Groups are isolated
usersValue, _ := users.Get("1")  // gets userData
postsValue, _ := posts.Get("1")  // gets postData

// Group with expiration
users.SetWithExpiration("2", userData2, time.Hour)

// Convenience methods
value := users.GetOrSet("3", defaultValue)
value = users.GetOrSetFunc("4", func() interface{} {
    // Fetch user from database
    return fetchUserFromDB("4")
})

// Clear the group
users.Clear()

// Batch deletions
// Delete keys by group prefix (recommended)
deleted := users.DeletePrefix("session:")
// Delete specific keys
deleted = users.DeleteKeys([]string{"1", "2"})
```

#### 4. Type-Safe Get Operations

`GetTyped` is a utility function to retrieve values with type safety using generics:

```go
// Store a string value
cache.Set("key", "value")

// Retrieve the string value    
value, exists := shardedcache.GetTyped[string](cache, "key")
if !exists {
    fmt.Println("key not found")
}
fmt.Println(value) // value is of type string

// Store a custom struct
type User struct {
    Name string
    Age int
}
cache.Set("key", User{Name: "John", Age: 30})

// Retrieve the custom struct with type safety
value, exists = shardedcache.GetTyped[User](cache, "key")
if !exists {
    fmt.Println("key not found")
}
fmt.Println(value) // type-safe access to User struct

// If type is not found, it will return the zero value of the type and false
value, exists = shardedcache.GetTyped[int](cache, "key")
fmt.Println(value) // 0, false
```

### Configuration Options

```go
type Options struct {
    MaxSize           int           // Maximum items per shard (0 = unlimited)
    DefaultExpiration time.Duration // Default item expiration time (0 = no expiration)
    CleanupInterval   time.Duration // Interval for cleaning expired items (0 = disable auto cleanup, defaults to 11m)
    PersistPath       string        // File path for persistence storage (empty = disable persistence)
    PersistInterval   time.Duration // Interval for data persistence (0 = disable auto persistence, defaults to 13m)
    PersistThreshold  int           // Number of changes before triggering persistence (0 = persist every change, defaults to 100)
    ShardCount        int           // Number of cache shards, must be power of 2 (defaults to 32)
}
```

## Contributing

Contributions are welcome! Please feel free to submit a pull request or open an issue.

## License

This project is licensed under the MIT License.