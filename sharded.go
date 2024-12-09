package cache

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cespare/xxhash/v2"
)

// ShardedCache represents a sharded cache implementation with multiple independent cache shards
type ShardedCache struct {
	shards    []*cacheShard // Array of cache shards
	shardMask uint64        // Mask used for shard selection
	shardNum  uint64        // Number of shards (power of 2)
	opts      Options       // Cache configuration options
}

// NewCache creates a new sharded cache instance
func NewCache(opts ...Options) CacheInterface {
	options := Options{
		MaxSize:           0,                // 0 means no limit
		DefaultExpiration: 0,                // 0 means no expiration
		CleanupInterval:   time.Minute * 11, // Default cleanup interval
		PersistInterval:   time.Minute * 13, // Default persistence interval
		PersistThreshold:  100,              // Default persistence threshold
		ShardCount:        32,               // Default number of shards
	}

	if len(opts) > 0 {
		options = opts[0]
		// Handle negative values, use defaults
		if options.MaxSize < 0 {
			options.MaxSize = 0
		}
		if options.DefaultExpiration < 0 {
			options.DefaultExpiration = 0
		}
		if options.CleanupInterval < 0 {
			options.CleanupInterval = time.Minute * 11
		}
		if options.PersistInterval < 0 {
			options.PersistInterval = time.Minute * 13
		}
		if options.PersistThreshold < 0 {
			options.PersistThreshold = 100
		}
		if options.ShardCount <= 0 {
			options.ShardCount = 32
		}
	}

	// Ensure the number of shards is a power of 2
	shardNum := nextPowerOf2(uint64(options.ShardCount))

	sc := &ShardedCache{
		shards:    make([]*cacheShard, shardNum),
		shardMask: shardNum - 1,
		shardNum:  shardNum,
		opts:      options,
	}

	// If persist path is set, ensure the directory exists
	if options.PersistPath != "" {
		// Convert PersistPath to an absolute path
		absPath, err := filepath.Abs(options.PersistPath)
		if err != nil {
			log.Fatalf("Failed to get absolute path: %v", err)
		}
		options.PersistPath = absPath

		if err := os.MkdirAll(options.PersistPath, 0755); err != nil {
			log.Fatalf("Failed to create persistence directory: %v", err)
		}
	}

	// Initialize each shard
	for i := uint64(0); i < shardNum; i++ {
		shardPath := ""
		if options.PersistPath != "" {
			// Create a separate persistence file for each shard
			shardPath = filepath.Join(options.PersistPath, fmt.Sprintf("%d.dat", i))
		}
		sc.shards[i] = newCacheShard(options, shardPath)
	}

	return sc
}

// getShard returns the cache shard for the given key
func (sc *ShardedCache) getShard(key string) *cacheShard {
	hash := xxhash.Sum64String(key)
	return sc.shards[hash&sc.shardMask]
}

// Set implements CacheInterface
func (sc *ShardedCache) Set(key string, value interface{}) {
	sc.getShard(key).set(key, value)
}

// SetWithExpiration implements CacheInterface
func (sc *ShardedCache) SetWithExpiration(key string, value interface{}, expiration time.Duration) {
	sc.getShard(key).setWithExpiration(key, value, expiration)
}

// Get implements CacheInterface
func (sc *ShardedCache) Get(key string) (interface{}, bool) {
	return sc.getShard(key).get(key)
}

// GetWithExpiration implements CacheInterface
func (sc *ShardedCache) GetWithExpiration(key string) (interface{}, *time.Time, bool) {
	return sc.getShard(key).getWithExpiration(key)
}

// Delete implements CacheInterface
func (sc *ShardedCache) Delete(key string) error {
	return sc.getShard(key).delete(key)
}

// GetOrSet implements CacheInterface
func (sc *ShardedCache) GetOrSet(key string, value interface{}) interface{} {
	if val, exists := sc.Get(key); exists {
		return val
	}
	sc.Set(key, value)
	return value
}

// GetOrSetFunc implements CacheInterface
func (sc *ShardedCache) GetOrSetFunc(key string, f func() interface{}) interface{} {
	return sc.GetOrSetFuncWithExpiration(key, f, sc.opts.DefaultExpiration)
}

// GetOrSetFuncWithExpiration implements CacheInterface
func (sc *ShardedCache) GetOrSetFuncWithExpiration(key string, f func() interface{}, expiration time.Duration) interface{} {
	shard := sc.getShard(key)

	// Try to get the value first
	if val, exists := shard.get(key); exists {
		return val
	}

	// If not found, use a mutex to ensure the function is only called once
	shard.mu.Lock()
	defer shard.mu.Unlock()

	// Double-check, but this time directly accessing the items map
	if item, found := shard.items[key]; found {
		if item.ExpireTime == nil || time.Now().Before(*item.ExpireTime) {
			atomic.AddUint64(&shard.hits, 1)
			return item.Value
		}
	}

	// Call the function to get the value
	value := f()
	shard.setWithExpirationUnlocked(key, value, expiration)
	return value
}

// Stats implements CacheInterface
func (sc *ShardedCache) Stats() map[string]interface{} {
	totalCount := 0
	totalHits := uint64(0)
	totalMisses := uint64(0)

	for _, shard := range sc.shards {
		shard.mu.RLock()
		totalCount += len(shard.items)
		hits := atomic.LoadUint64(&shard.hits)
		misses := atomic.LoadUint64(&shard.misses)
		shard.mu.RUnlock()

		totalHits += hits
		totalMisses += misses
	}

	hitRate := float64(0)
	if total := totalHits + totalMisses; total > 0 {
		hitRate = float64(totalHits) / float64(total) * 100
	}

	// Basic statistics
	stats := map[string]interface{}{
		"count":   totalCount,
		"shards":  sc.shardNum,
		"hits":    totalHits,
		"misses":  totalMisses,
		"hitRate": fmt.Sprintf("%.2f%%", hitRate),
	}

	// Add configuration information
	stats["maxSize"] = sc.opts.MaxSize
	stats["defaultExpiration"] = sc.opts.DefaultExpiration
	stats["cleanupInterval"] = sc.opts.CleanupInterval
	stats["persistInterval"] = sc.opts.PersistInterval
	stats["persistThreshold"] = sc.opts.PersistThreshold
	stats["shardCount"] = sc.shardNum
	stats["persistPath"] = sc.opts.PersistPath

	return stats
}

// OnEvicted implements CacheInterface
func (sc *ShardedCache) OnEvicted(f func(key string, value interface{})) {
	for _, shard := range sc.shards {
		shard.mu.Lock()
		shard.onEvicted = f
		shard.mu.Unlock()
	}
}

// Keys implements CacheInterface
func (sc *ShardedCache) Keys() []string {
	keys := make([]string, 0)
	for _, shard := range sc.shards {
		shard.mu.RLock()
		for key := range shard.items {
			keys = append(keys, key)
		}
		shard.mu.RUnlock()
	}
	return keys
}

// Count implements CacheInterface
func (sc *ShardedCache) Count() int {
	count := 0
	for _, shard := range sc.shards {
		shard.mu.RLock()
		count += len(shard.items)
		shard.mu.RUnlock()
	}
	return count
}

// Has implements CacheInterface
func (sc *ShardedCache) Has(key string) bool {
	_, exists := sc.Get(key)
	return exists
}

// Clear implements CacheInterface
func (sc *ShardedCache) Clear() error {
	for _, shard := range sc.shards {
		shard.mu.Lock()
		for key, item := range shard.items {
			if shard.onEvicted != nil {
				shard.onEvicted(key, item.Value)
			}
			delete(shard.items, key)
		}
		atomic.StoreUint64(&shard.hits, 0)
		atomic.StoreUint64(&shard.misses, 0)
		shard.mu.Unlock()
	}
	return nil
}

// Close implements CacheInterface
func (sc *ShardedCache) Close() {
	var wg sync.WaitGroup
	for _, shard := range sc.shards {
		wg.Add(1)
		go func(shard *cacheShard) {
			defer wg.Done()
			shard.close()
		}(shard)
	}
	wg.Wait()
}

// nextPowerOf2 returns the smallest power of 2 that is greater than or equal to v
// Examples:
// Input: 7  -> Output: 8
// Input: 8  -> Output: 8
// Input: 9  -> Output: 16
// Input: 33 -> Output: 64
func nextPowerOf2(v uint64) uint64 {
	v--
	v |= v >> 1
	v |= v >> 2
	v |= v >> 4
	v |= v >> 8
	v |= v >> 16
	v++
	return v
}
