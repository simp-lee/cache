// Package shardedcache provides a sharded in-memory cache with expiration capabilities and persistence.
package shardedcache

import (
	"sync"
	"time"
)

type CacheInterface interface {
	Set(key string, value interface{})
	SetWithExpiration(key string, value interface{}, expiration time.Duration)
	Get(key string) (interface{}, bool)
	GetWithExpiration(key string) (interface{}, *time.Time, bool)
	Delete(key string) bool
	DeleteKeys(keys []string) int
	DeletePrefix(prefix string) int
	GetOrSet(key string, value interface{}) interface{}
	GetOrSetFunc(key string, f func() interface{}) interface{}
	GetOrSetFuncWithExpiration(key string, f func() interface{}, expiration time.Duration) interface{}
	Stats() map[string]interface{}
	OnEvicted(f func(key string, value interface{}))
	Keys() []string
	Count() int
	Has(key string) bool
	Clear()
	Close()
	Group(name string) Group
}

type Options struct {
	MaxSize           int           // Maximum items per shard (0 = unlimited)
	DefaultExpiration time.Duration // Default item expiration time (0 = no expiration)
	CleanupInterval   time.Duration // Interval for cleaning expired items (0 = disable auto cleanup, defaults to 11m)
	PersistPath       string        // File path for persistence storage (empty = disable persistence)
	PersistInterval   time.Duration // Interval for data persistence (0 = disable auto persistence, defaults to 13m)
	PersistThreshold  int           // Number of changes before triggering persistence (0 = persist every change, defaults to 100)
	ShardCount        int           // Number of cache shards, must be power of 2 (defaults to 32)
}

var (
	defaultCache CacheInterface
	once         sync.Once
)

// Init initializes the default cache instance (singleton pattern)
func Init(opts ...Options) {
	once.Do(func() {
		defaultCache = NewCache(opts...)
	})
}

// Get returns the default cache instance
func Get() CacheInterface {
	if defaultCache == nil {
		panic("cache not initialized")
	}
	return defaultCache
}

// GetTyped returns the cache value of the specified type
func GetTyped[T any](c CacheInterface, key string) (T, bool) {
	var zero T
	if value, exists := c.Get(key); exists {
		if typedValue, ok := value.(T); ok {
			return typedValue, true
		}
	}
	return zero, false
}
