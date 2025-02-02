package shardedcache

import "time"

type Group interface {
	// Basic operations
	Set(key string, value interface{})
	SetWithExpiration(key string, value interface{}, expiration time.Duration)
	Get(key string) (interface{}, bool)
	GetWithExpiration(key string) (interface{}, *time.Time, bool)
	Delete(key string) error

	// Utility operations
	GetOrSet(key string, value interface{}) interface{}
	GetOrSetFunc(key string, f func() interface{}) interface{}
	GetOrSetFuncWithExpiration(key string, f func() interface{}, expiration time.Duration) interface{}

	// Group operations
	Keys() []string
	Count() int
	Clear() error
}

const groupKeySeparator = "\x00:" // Use NULL character as separator prefix

// cacheGroup implements the Group interface
type cacheGroup struct {
	cache     *ShardedCache
	groupName string
}

// Implement Group interface methods
func (g *cacheGroup) buildKey(key string) string {
	return g.groupName + groupKeySeparator + key
}

func (g *cacheGroup) Set(key string, value interface{}) {
	g.cache.Set(g.buildKey(key), value)
}

func (g *cacheGroup) SetWithExpiration(key string, value interface{}, expiration time.Duration) {
	g.cache.SetWithExpiration(g.buildKey(key), value, expiration)
}

func (g *cacheGroup) Get(key string) (interface{}, bool) {
	return g.cache.Get(g.buildKey(key))
}

func (g *cacheGroup) GetWithExpiration(key string) (interface{}, *time.Time, bool) {
	return g.cache.GetWithExpiration(g.buildKey(key))
}

func (g *cacheGroup) Delete(key string) error {
	return g.cache.Delete(g.buildKey(key))
}

func (g *cacheGroup) GetOrSet(key string, value interface{}) interface{} {
	return g.cache.GetOrSet(g.buildKey(key), value)
}

func (g *cacheGroup) GetOrSetFunc(key string, f func() interface{}) interface{} {
	return g.cache.GetOrSetFunc(g.buildKey(key), f)
}

func (g *cacheGroup) GetOrSetFuncWithExpiration(key string, f func() interface{}, expiration time.Duration) interface{} {
	return g.cache.GetOrSetFuncWithExpiration(g.buildKey(key), f, expiration)
}

func (g *cacheGroup) Keys() []string {
	groupPrefix := g.groupName + groupKeySeparator
	var keys []string

	allKeys := g.cache.Keys()
	for _, key := range allKeys {
		if len(key) >= len(groupPrefix) && key[:len(groupPrefix)] == groupPrefix {
			// Return the key without the group prefix
			keys = append(keys, key[len(groupPrefix):])
		}
	}

	return keys
}

func (g *cacheGroup) Count() int {
	return len(g.Keys())
}

func (g *cacheGroup) Clear() error {
	keys := g.cache.Keys()
	groupPrefix := g.groupName + groupKeySeparator

	for _, key := range keys {
		if len(key) >= len(groupPrefix) && key[:len(groupPrefix)] == groupPrefix {
			if err := g.cache.Delete(key); err != nil {
				return err
			}
		}
	}

	return nil
}
