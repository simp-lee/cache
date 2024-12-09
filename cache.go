// Package cache provides a sharded in-memory cache with expiration capabilities and persistence.
package cache

import (
	"sync"
	"time"
)

type CacheInterface interface {
	Set(key string, value interface{})
	SetWithExpiration(key string, value interface{}, expiration time.Duration)
	Get(key string) (interface{}, bool)
	GetWithExpiration(key string) (interface{}, *time.Time, bool)
	Delete(key string) error
	GetOrSet(key string, value interface{}) interface{}
	GetOrSetFunc(key string, f func() interface{}) interface{}
	GetOrSetFuncWithExpiration(key string, f func() interface{}, expiration time.Duration) interface{}
	Stats() map[string]interface{}
	OnEvicted(f func(key string, value interface{}))
	Keys() []string
	Count() int
	Has(key string) bool
	Clear() error
	Close()
}

type Options struct {
	MaxSize           int           // Maximum number of items per shard, 0 means unlimited
	DefaultExpiration time.Duration // Default expiration time, 0 means never expire
	CleanupInterval   time.Duration // Cleanup interval, 0 means no automatic cleanup
	PersistPath       string        // Persistence file path
	PersistInterval   time.Duration // Persistence interval
	PersistThreshold  int           // Incremental persistence threshold (modification count)
	ShardCount        int           // Number of shards, must be a power of 2
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
