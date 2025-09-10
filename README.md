# Sharded In-Memory Cache

A high-performance sharded in-memory cache for Go with group namespaces, disk persistence, and comprehensive cache operations. Designed for concurrent access with excellent performance characteristics.

## Features

- **Persistence Support**: Disk persistence and automatic reload on startup  
- **Type Safety**: Generic `GetTyped[T]()` for type-safe value retrieval
- **Group Support**: Namespace management for organized cache operations
- **Expiration Control**: Flexible expiration with `NoExpiration` and `DefaultExpiration` constants
- **Statistics**: Detailed cache statistics including hit rates and item counts
- **Eviction Callbacks**: Custom handlers for item eviction events
- **Batch Operations**: Efficient bulk delete operations with prefix/key matching

## Installation

```bash
go get github.com/simp-lee/cache
```

## Quick Start

### Basic Usage

```go
package main

import (
    "fmt"
    "time"
    shardedcache "github.com/simp-lee/cache"
)

func main() {
    // Create cache instance
    cache := shardedcache.NewCache(shardedcache.Options{
        MaxSize:           1000,
        DefaultExpiration: time.Minute * 5,
        ShardCount:        32, // Will auto-adjust to nearest power of two if needed
    })

    // Basic operations
    cache.Set("key", "value")
    value, exists := cache.Get("key")
    if exists {
        fmt.Println(value) // Output: value
    }

    // With expiration
    cache.SetWithExpiration("temp", "data", time.Hour)
    
    // Type-safe retrieval
    str, exists := shardedcache.GetTyped[string](cache, "key")
    
    // Utility functions
    result := cache.GetOrSet("new_key", "default_value")
    result = cache.GetOrSetFunc("computed", func() interface{} {
        return "expensive computation"
    })
}
```

### Singleton Pattern

```go
// Initialize once at startup
shardedcache.Init(shardedcache.Options{
    DefaultExpiration: time.Hour,
    ShardCount:        64,
})

// Use anywhere in your application
cache := shardedcache.Get()
cache.Set("global_key", "value")
```

### Expiration & Cleanup

```go
// Expiration constants
const (
    NoExpiration      time.Duration = -1 // Item never expires
    DefaultExpiration time.Duration = 0  // Use cache's default expiration
)

// Usage examples
cache := shardedcache.NewCache(shardedcache.Options{
    DefaultExpiration: time.Hour,
    // CleanupInterval > 0 starts a background goroutine that periodically removes expired entries.
    // Setting CleanupInterval to 0 disables periodic cleanup (access still lazily removes expired items).
})

cache.SetWithExpiration("permanent", "value", shardedcache.NoExpiration)
cache.SetWithExpiration("default", "value", shardedcache.DefaultExpiration)
cache.SetWithExpiration("custom", "value", time.Minute*30)

// Safe expiration time access (returns time.Time values, not pointers)
value, expTime, exists := cache.GetWithExpiration("key")
if exists {
    if !expTime.IsZero() { // NoExpiration will yield zero time
        remaining := time.Until(expTime)
        fmt.Printf("Expires in: %v\n", remaining)
    }
    _ = value
}

// Expired entries are:
// 1) Removed lazily on access (Get / GetWithExpiration)
// 2) Removed eagerly by the periodic cleaner if CleanupInterval > 0
```

### Group Support

Groups provide namespace isolation for cache keys with (practically) zero additional memory (only a tiny wrapper object; no separate storage structure):

```go
// Create groups for different data types
users := cache.Group("users")
posts := cache.Group("posts")

// Groups are logically isolated (implemented via key prefix `<group>\x00:`)
users.Set("1", userData)
posts.Set("1", postData)  // Different from users.Get("1")

// All cache operations available in groups
users.SetWithExpiration("session:123", sessionData, time.Hour)
value := users.GetOrSetFunc("profile:456", func() interface{} {
    return fetchUserProfile("456")
})

// Group-specific operations
users.Clear()                                    // Clear only user data
deleted := users.DeletePrefix("session:")        // Delete user sessions
deleted = users.DeleteKeys([]string{"1", "2"})   // Delete specific users
```

### Advanced Features

#### Type-Safe Operations
```go
// Store typed data
cache.Set("user", User{Name: "John", Age: 30})

// Retrieve with type safety
user, exists := shardedcache.GetTyped[User](cache, "user")
if exists {
    fmt.Println(user.Name) // Direct access to User fields
}
```

#### Statistics and Monitoring
```go
// Set eviction callback
cache.OnEvicted(func(key string, value interface{}) {
    log.Printf("Item evicted: %s", key)
})

// Get cache statistics (keys are camelCase)
stats := cache.Stats()
// Provided fields:
// count, hits, misses, hitRate,
// maxSize, defaultExpiration, cleanupInterval,
// persistInterval, persistThreshold, shardCount, persistPath
fmt.Printf("Hit rate: %.2f\n", stats["hitRate"]) // hits / (hits + misses)
fmt.Printf("Total items: %d\n", cache.Count())
```

#### Persistence
```go
cache := shardedcache.NewCache(shardedcache.Options{
    PersistPath:      "/tmp/cachedata", // Directory path, NOT a single file
    PersistInterval:  time.Minute * 5,   // Periodic flush (0 disables; negative input coerced to default 13m)
    PersistThreshold: 100,               // Flush after N mutations (0 = every change; negative coerced to 100)
})
// Persistence details:
// - A directory is created if missing.
// - Each shard stores its own file: <PersistPath>/<shardIndex>.dat
// - Triggers:
//   * Threshold-based (mutations reach PersistThreshold)  -- requires PersistInterval > 0 (persister goroutine must be running)
//   * Time-based (ticker at PersistInterval if there are changes)
//   * Final flush on Close()
// - Loading: On startup each shard attempts to load its corresponding file.
// - Encoding: Go gob (interface{}). For custom concrete types register them, e.g.:
//     gob.Register(MyStruct{})
// Recommendation: Call Close() on graceful shutdown to ensure last changes are flushed.
```

### Concurrency & Singleflight-like Behavior

`GetOrSetFunc` / `GetOrSetFuncWithExpiration` avoid duplicate computation for the same key via shard-level locking (not per-key lock). A shard lock means different keys on the same shard may serialize during the computation window; once value stored, later goroutines read it.

### Eviction & Capacity

`MaxSize` applies per shard (effective total capacity = MaxSize * shardCount). When adding a new key beyond capacity, the oldest (by creation time) item in that shard is evicted. This is a simple FIFO-by-insert-time policy (not LRU). For LRU/FIFO policy sophistication, see SwiftCache.

### Key Enumeration Complexity

`Keys()` and `Count()` iterate all shards (O(N)). Group `Count()` first builds the group's key list (also O(N)). Avoid invoking them on very hot paths for extremely large caches.

## Configuration

```go
type Options struct {
    MaxSize           int           // Max items per shard (0 = unlimited; negative -> coerced to 0)
    DefaultExpiration time.Duration // Default expiration (<=0 -> no expiration; negative coerced to 0)
    CleanupInterval   time.Duration // Background expiry cleanup (0 disables; negative input coerced to default 11m)
    PersistPath       string        // Directory path for shard files (empty -> disable persistence)
    PersistInterval   time.Duration // Periodic persistence (0 disables; negative input coerced to default 13m)
    PersistThreshold  int           // Mutations threshold (0 -> every change; negative coerced to 100; threshold requires PersistInterval > 0 to flush immediately)
    ShardCount        int           // Desired shard count (auto-rounded up to next power of 2; <=0 -> default 32)
}

Behavior Summary:
- Shard auto-adjust: e.g. 33 -> 64
- Negative CleanupInterval/PersistInterval are coerced to defaults (11m / 13m) rather than disabling
- PersistThreshold only effective during runtime if PersistInterval > 0 (persister goroutine active)
- Negative values for sizes/thresholds coerced to safe defaults
- Persistence stores per-shard gob files
- Close() flushes pending changes
- Clear() resets hits & misses counters and triggers eviction callbacks for each item
```

## API Reference

### Signatures (concise overview)

```go
// Core
Set(key string, value interface{})
SetWithExpiration(key string, value interface{}, expiration time.Duration)
Get(key string) (value interface{}, found bool)
GetWithExpiration(key string) (value interface{}, expireTime time.Time, found bool)
Delete(key string) bool
Has(key string) bool
Clear()                  // Clears all cache items and resets hit/miss counters
Close()                  // Flush persistence & stop background goroutines

// Batch & Introspection
DeleteKeys(keys []string) (deleted int)
DeletePrefix(prefix string) (deleted int)   // prefix == "" -> no-op (use Clear())
Keys() []string
Count() int
Stats() map[string]interface{}              // Fields: count,hits,misses,hitRate,maxSize,defaultExpiration,cleanupInterval,persistInterval,persistThreshold,shardCount,persistPath
OnEvicted(cb func(key string, value interface{}))

// Get-Or-Set helpers (single computation per key per shard)
GetOrSet(key string, defaultValue interface{}) interface{}
GetOrSetFunc(key string, f func() interface{}) interface{}
GetOrSetFuncWithExpiration(key string, f func() interface{}, expiration time.Duration) interface{}

// Generic helper
GetTyped[T any](c CacheInterface, key string) (val T, found bool) // returns zero T + false on miss or type mismatch

// Group interface (obtained via cache.Group(name))
type Group interface {
    Set(key string, value interface{})
    SetWithExpiration(key string, value interface{}, expiration time.Duration)
    Get(key string) (interface{}, bool)
    GetWithExpiration(key string) (interface{}, time.Time, bool)
    Delete(key string) bool
    DeleteKeys(keys []string) int
    DeletePrefix(prefix string) int
    GetOrSet(key string, defaultValue interface{}) interface{}
    GetOrSetFunc(key string, f func() interface{}) interface{}
    GetOrSetFuncWithExpiration(key string, f func() interface{}, expiration time.Duration) interface{}
    Keys() []string
    Count() int
    Has(key string) bool
    Clear()
}
```

### Notes
- Groups are implemented by prefixing keys with `<group>\x00:`; avoid raw keys containing `\x00:` if possible.
- Calling `shardedcache.Get()` before `Init()` will panic.
- Eviction policy when `MaxSize` exceeded is oldest-by-creation (FIFO per shard).
- Shard count auto-rounded up to the next power of two.
- Persistence writes one gob file per shard inside the provided directory.
- sync.Pool reduces allocations for items & time objects.

## Performance

Designed for high efficiency and performance in concurrent environments. The benchmarks below were produced on the following test environment:

**Test Environment:**
- **CPU**: 13th Gen Intel(R) Core(TM) i5-13500H
- **OS**: Windows 11 (windows/amd64)
- **Go Version**: go1.24.6
- **GOMAXPROCS**: 16 (default)
- **Cores**: 16 logical processors

Minor variations are expected across different hardware and Go versions.

## Performance Comparison

Benchmark results comparing our `ShardedCache` with [go-cache](https://github.com/patrickmn/go-cache), [bigcache](https://github.com/allegro/bigcache) and sync.Map:

### Get Operations
| Implementation | ops/s | ns/op | Hit Rate | B/op | allocs/op |
|----------------|-------|-------|----------|------|-----------|
| ShardedCache   | 22,687,654 | 52.25 | 100% | 23 | 1 |
| GoCache        | 16,722,361 | 70.87 | 100% | 23 | 1 |
| SyncMap        | 23,469,448 | 51.16 | 100% | 23 | 1 |
| BigCache       | 22,618,719 | 55.79 | 100% | 54 | 3 |

### Set Operations
| Implementation | ops/s | ns/op | B/op | allocs/op |
|----------------|-------|-------|------|-----------|
| ShardedCache   | 16,102,454 | 85.04 | 75  | 5 |
| GoCache        | 3,581,130  | 395.70 | 63 | 4 |
| SyncMap        | 4,562,907  | 304.90 | 48 | 3 |
| BigCache       | 30,792,758 | 71.15  | 186 | 4 |

### Mixed Operations (80% Get, 20% Set)
| Implementation | ops/s | ns/op | Hit Rate | B/op | allocs/op |
|----------------|-------|-------|----------|------|-----------|
| ShardedCache   | 18,508,464 | 65.68 | 100% | 31 | 2 |
| GoCache        | 2,130,778  | 569.80 | 100% | 31 | 2 |
| SyncMap        | 2,105,696  | 567.90 | 100% | 47 | 3 |
| BigCache       | 22,440,392 | 54.70  | 100% | 53 | 3 |

### Memory Usage
Memory efficiency comparison with 1 million items (approximate, depends on key/value shapes):

| Implementation | Approx Memory |
|----------------|---------------|
| ShardedCache   | 169.71 MB |
| GoCache        | 135.03 MB |
| SyncMap        | 102.77 MB |
| BigCache       | 337.07 MB |

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

## Related Projects

For advanced LRU/FIFO eviction policies, see [SwiftCache](https://github.com/simp-lee/SwiftCache). This project uses a simpler creation-time FIFO eviction when `MaxSize` is exceeded.

## License

MIT License