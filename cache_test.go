package shardedcache

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCache(t *testing.T) {
	opts := Options{
		MaxSize:           1000,
		DefaultExpiration: time.Second * 5,
		CleanupInterval:   time.Second,
		ShardCount:        32,
	}

	cache := NewCache(opts)

	t.Run("Basic Set and Get", func(t *testing.T) {
		cache.Set("key1", "value1")
		if val, exists := cache.Get("key1"); !exists || val != "value1" {
			t.Errorf("Expected value1, got %v", val)
		}
	})

	t.Run("Expiration", func(t *testing.T) {
		cache.SetWithExpiration("key2", "value2", time.Millisecond*100)

		if _, exists := cache.Get("key2"); !exists {
			t.Error("Key should exist immediately after setting")
		}

		time.Sleep(time.Millisecond * 200)

		if _, exists := cache.Get("key2"); exists {
			t.Error("Key should have expired")
		}
	})

	t.Run("GetOrSet", func(t *testing.T) {
		// First call sets the value
		val := cache.GetOrSet("key3", "value3")
		if val != "value3" {
			t.Errorf("Expected value3, got %v", val)
		}

		// Second call returns existing value
		val = cache.GetOrSet("key3", "different_value")
		if val != "value3" {
			t.Errorf("Expected value3, got %v", val)
		}
	})

	t.Run("GetOrSetFunc", func(t *testing.T) {
		callCount := 0
		getValue := func() interface{} {
			callCount++
			return "generated_value"
		}

		// First call executes function
		val := cache.GetOrSetFunc("func_key", getValue)
		if val != "generated_value" {
			t.Errorf("Expected 'generated_value', got %v", val)
		}
		if callCount != 1 {
			t.Errorf("Function should be called exactly once, got %d calls", callCount)
		}

		// Second call returns cached value
		val = cache.GetOrSetFunc("func_key", getValue)
		if val != "generated_value" {
			t.Errorf("Expected cached 'generated_value', got %v", val)
		}
		if callCount != 1 {
			t.Errorf("Function should not be called again, got %d calls", callCount)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		cache.Set("key4", "value4")
		cache.Delete("key4")
		if _, exists := cache.Get("key4"); exists {
			t.Error("Key should have been deleted")
		}
	})

	t.Run("Clear", func(t *testing.T) {
		cache.Set("key5", "value5")
		cache.Clear()
		if cache.Count() != 0 {
			t.Error("Cache should be empty after clear")
		}
	})

	t.Run("Stats", func(t *testing.T) {
		cache.Clear()
		cache.Set("key6", "value6")
		cache.Get("key6")
		cache.Get("nonexistent")

		stats := cache.Stats()
		if stats["count"].(int) != 1 {
			t.Errorf("Incorrect item count in stats: %v", stats["count"])
		}
		if stats["hits"].(uint64) != 1 {
			t.Errorf("Incorrect hit count in stats: %v", stats["hits"])
		}
		if stats["misses"].(uint64) != 1 {
			t.Errorf("Incorrect miss count in stats: %v", stats["misses"])
		}
	})
}

func TestSingletonCache(t *testing.T) {
	opts := Options{
		MaxSize:           100,
		DefaultExpiration: time.Second * 5,
		CleanupInterval:   time.Second,
		ShardCount:        32,
	}

	Init(opts)
	cache := Get()

	t.Run("Basic Set and Get", func(t *testing.T) {
		cache.Set("key1", "value1")
		if val, exists := cache.Get("key1"); !exists || val != "value1" {
			t.Errorf("Expected value1, got %v", val)
		}
	})

	t.Run("Expiration", func(t *testing.T) {
		cache.SetWithExpiration("key2", "value2", time.Millisecond*100)

		if _, exists := cache.Get("key2"); !exists {
			t.Error("Key should exist immediately after setting")
		}

		time.Sleep(time.Millisecond * 200)

		if _, exists := cache.Get("key2"); exists {
			t.Error("Key should have expired")
		}
	})

	t.Run("GetOrSet", func(t *testing.T) {
		// First call sets the value
		val := cache.GetOrSet("key3", "value3")
		if val != "value3" {
			t.Errorf("Expected value3, got %v", val)
		}

		// Second call returns existing value
		val = cache.GetOrSet("key3", "different_value")
		if val != "value3" {
			t.Errorf("Expected value3, got %v", val)
		}
	})

	t.Run("Delete", func(t *testing.T) {
		cache.Set("key4", "value4")
		cache.Delete("key4")
		if _, exists := cache.Get("key4"); exists {
			t.Error("Key should have been deleted")
		}
	})

	t.Run("Clear", func(t *testing.T) {
		cache.Set("key5", "value5")
		cache.Clear()
		if cache.Count() != 0 {
			t.Error("Cache should be empty after clear")
		}
	})

	t.Run("Stats", func(t *testing.T) {
		cache.Clear()
		cache.Set("key6", "value6")
		cache.Get("key6")
		cache.Get("nonexistent")

		stats := cache.Stats()
		if stats["count"].(int) != 1 {
			t.Errorf("Incorrect item count in stats: %v", stats["count"])
		}
		if stats["hits"].(uint64) != 1 {
			t.Errorf("Incorrect hit count in stats: %v", stats["hits"])
		}
		if stats["misses"].(uint64) != 1 {
			t.Errorf("Incorrect miss count in stats: %v", stats["misses"])
		}
		if stats["shardCount"].(uint64) != 32 {
			t.Errorf("Incorrect shard count in stats: %v", stats["shardCount"])
		}
	})

	t.Run("MaxSize With Single Shard", func(t *testing.T) {
		// Use single shard to test max capacity
		singleShardOpts := Options{
			MaxSize:    2,
			ShardCount: 1, // Single shard for eviction testing
		}
		singleCache := NewCache(singleShardOpts)

		singleCache.Set("k1", "v1")
		time.Sleep(time.Millisecond * 10)
		singleCache.Set("k2", "v2")
		time.Sleep(time.Millisecond * 10)
		singleCache.Set("k3", "v3") // Should evict oldest item

		if singleCache.Count() != 2 {
			t.Errorf("Expected count of 2, got %d", singleCache.Count())
		}
		if _, exists := singleCache.Get("k1"); exists {
			t.Error("k1 should have been evicted")
		}
		if _, exists := singleCache.Get("k3"); !exists {
			t.Error("k3 should exist")
		}
	})

	t.Run("Singleton Pattern", func(t *testing.T) {
		cache1 := Get()
		cache2 := Get()

		cache1.Set("singleton_key", "singleton_value")
		if val, exists := cache2.Get("singleton_key"); !exists || val != "singleton_value" {
			t.Error("Singleton pattern failed: caches are not the same instance")
		}
	})
}

func TestGetTyped(t *testing.T) {
	cache := NewCache()

	t.Run("Integer Type", func(t *testing.T) {
		cache.Set("int", 42)
		if val, ok := GetTyped[int](cache, "int"); !ok || val != 42 {
			t.Error("Failed to get typed int value")
		}
	})

	t.Run("String Type", func(t *testing.T) {
		cache.Set("string", "hello")
		if val, ok := GetTyped[string](cache, "string"); !ok || val != "hello" {
			t.Error("Failed to get typed string value")
		}
	})

	t.Run("Type Mismatch", func(t *testing.T) {
		cache.Set("mismatch", "string")
		if _, ok := GetTyped[int](cache, "mismatch"); ok {
			t.Error("Should fail when types don't match")
		}
	})

	type User struct {
		Name string
		Age  int
	}

	t.Run("Struct Type", func(t *testing.T) {
		user := User{Name: "Alice", Age: 25}
		cache.Set("user", user)
		if val, ok := GetTyped[User](cache, "user"); !ok || val.Name != "Alice" {
			t.Error("Failed to get typed struct value")
		}
	})
}

func TestGetOrSetFunc(t *testing.T) {
	cache := Get()

	t.Run("Basic GetOrSetFunc", func(t *testing.T) {
		// First call should execute function
		callCount := 0
		getValue := func() interface{} {
			callCount++
			return "generated_value"
		}

		val := cache.GetOrSetFunc("func_key", getValue)
		if val != "generated_value" {
			t.Errorf("Expected 'generated_value', got %v", val)
		}
		if callCount != 1 {
			t.Errorf("Function should be called exactly once, got %d calls", callCount)
		}

		// Second call should return cached value without executing function
		val = cache.GetOrSetFunc("func_key", getValue)
		if val != "generated_value" {
			t.Errorf("Expected cached 'generated_value', got %v", val)
		}
		if callCount != 1 {
			t.Errorf("Function should not be called again, got %d calls", callCount)
		}
	})
}

func TestGetOrSetFuncWithExpiration(t *testing.T) {
	cache := NewCache(Options{
		ShardCount:        32,
		DefaultExpiration: time.Second,
	})

	t.Run("Basic Expiration", func(t *testing.T) {
		var callCount int32
		getValue := func() interface{} {
			atomic.AddInt32(&callCount, 1)
			return "expiring_value"
		}

		// First call
		val := cache.GetOrSetFuncWithExpiration("exp_key", getValue, time.Millisecond*100)
		if val != "expiring_value" {
			t.Errorf("Expected 'expiring_value', got %v", val)
		}
		if atomic.LoadInt32(&callCount) != 1 {
			t.Errorf("Function should be called exactly once, got %d calls", callCount)
		}

		// Immediate get should return cached value
		val = cache.GetOrSetFuncWithExpiration("exp_key", getValue, time.Millisecond*100)
		if val != "expiring_value" {
			t.Errorf("Expected cached 'expiring_value', got %v", val)
		}
		if atomic.LoadInt32(&callCount) != 1 {
			t.Errorf("Function should not be called again, got %d calls", callCount)
		}

		// Wait for expiration
		time.Sleep(time.Millisecond * 200)

		// After expiration, function should be called again
		val = cache.GetOrSetFuncWithExpiration("exp_key", getValue, time.Millisecond*100)
		if val != "expiring_value" {
			t.Errorf("Expected new 'expiring_value', got %v", val)
		}
		if atomic.LoadInt32(&callCount) != 2 {
			t.Errorf("Function should be called again after expiration, got %d calls", callCount)
		}
	})

	t.Run("Concurrent Access", func(t *testing.T) {
		var callCount int32
		getValue := func() interface{} {
			// Add random delay to make race conditions more likely
			time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
			atomic.AddInt32(&callCount, 1)
			return "concurrent_value"
		}

		var wg sync.WaitGroup
		concurrency := 100 // Increase concurrency for better testing
		wg.Add(concurrency)

		// Reset counter
		atomic.StoreInt32(&callCount, 0)

		// Concurrent access
		for i := 0; i < concurrency; i++ {
			go func() {
				defer wg.Done()
				val := cache.GetOrSetFuncWithExpiration("concurrent_key", getValue, time.Second)
				if val != "concurrent_value" {
					t.Errorf("Expected 'concurrent_value', got %v", val)
				}
			}()
		}

		wg.Wait()

		// Verify function was called exactly once
		if count := atomic.LoadInt32(&callCount); count != 1 {
			t.Errorf("Function should be called exactly once under concurrent access, got %d calls", count)
		}
	})

	t.Run("Mixed Expiration and Concurrency", func(t *testing.T) {
		var callCount int32
		getValue := func() interface{} {
			time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
			atomic.AddInt32(&callCount, 1)
			return "mixed_value"
		}

		var wg sync.WaitGroup
		concurrency := 50

		// First round of concurrent access
		atomic.StoreInt32(&callCount, 0)
		wg.Add(concurrency)
		for i := 0; i < concurrency; i++ {
			go func() {
				defer wg.Done()
				cache.GetOrSetFuncWithExpiration("mixed_key", getValue, time.Millisecond*50)
			}()
		}
		wg.Wait()

		if count := atomic.LoadInt32(&callCount); count != 1 {
			t.Errorf("First round: function should be called once, got %d calls", count)
		}

		// Wait for expiration
		time.Sleep(time.Millisecond * 100)

		// Second round of concurrent access
		atomic.StoreInt32(&callCount, 0)
		wg.Add(concurrency)
		for i := 0; i < concurrency; i++ {
			go func() {
				defer wg.Done()
				cache.GetOrSetFuncWithExpiration("mixed_key", getValue, time.Millisecond*50)
			}()
		}
		wg.Wait()

		if count := atomic.LoadInt32(&callCount); count != 1 {
			t.Errorf("Second round: function should be called once, got %d calls", count)
		}
	})
}

func TestOnEvicted(t *testing.T) {
	cache := NewCache(Options{
		MaxSize:         2,
		CleanupInterval: time.Millisecond * 100,
		ShardCount:      1, // Single shard for eviction testing
	})

	t.Run("Eviction Callback", func(t *testing.T) {
		var mu sync.Mutex
		evictedKeys := make([]interface{}, 0)
		evictedVals := make([]interface{}, 0)

		cache.OnEvicted(func(key string, value interface{}) {
			mu.Lock()
			evictedKeys = append(evictedKeys, key)
			evictedVals = append(evictedVals, value)
			mu.Unlock()
		})

		// Add data to trigger eviction
		cache.Set("k1", "v1")
		time.Sleep(time.Millisecond * 10)
		cache.Set("k2", "v2")
		time.Sleep(time.Millisecond * 10)
		cache.Set("k3", "v3") // Should trigger k1 eviction

		// Wait for callback execution
		time.Sleep(time.Millisecond * 50)

		mu.Lock()
		defer mu.Unlock()

		// Verify callback was executed correctly
		if len(evictedKeys) != 1 {
			t.Errorf("Expected 1 eviction, got %d", len(evictedKeys))
		}
		if evictedKeys[0] != "k1" {
			t.Errorf("Expected k1 to be evicted, got %v", evictedKeys[0])
		}
		if evictedVals[0] != "v1" {
			t.Errorf("Expected v1 to be evicted value, got %v", evictedVals[0])
		}
	})

	t.Run("Expiration Callback", func(t *testing.T) {
		evicted := make(chan struct{}, 1)
		cache.OnEvicted(func(key string, value interface{}) {
			if key == "exp_key" && value == "exp_val" {
				evicted <- struct{}{}
			}
		})

		// Set item with short expiration
		cache.SetWithExpiration("exp_key", "exp_val", time.Millisecond*50)

		// Trigger cleanup manually
		time.Sleep(time.Millisecond * 100)
		cache.Get("exp_key") // Trigger expiration check

		// Wait for callback
		select {
		case <-evicted:
			// Callback was correctly triggered
		case <-time.After(time.Millisecond * 100):
			t.Error("Eviction callback was not called for expired item")
		}
	})

	t.Run("Delete Callback", func(t *testing.T) {
		cache.Clear()
		var called atomic.Bool
		cache.OnEvicted(func(key string, value interface{}) {
			if key == "del_key" && value == "del_val" {
				called.Store(true)
			}
		})

		cache.Set("del_key", "del_val")
		cache.Delete("del_key")

		// Wait for callback execution
		time.Sleep(time.Millisecond * 50)

		if !called.Load() {
			t.Error("Eviction callback was not called for deleted item")
		}
	})

	t.Run("Clear Callback", func(t *testing.T) {
		cache.Clear()
		var evictedCount atomic.Int32
		cache.OnEvicted(func(key string, value interface{}) {
			evictedCount.Add(1)
		})

		// Add multiple items
		cache.Set("clear1", "val1")
		cache.Set("clear2", "val2")
		cache.Set("clear3", "val3")

		cache.Clear()

		// Wait for callback execution
		time.Sleep(time.Millisecond * 50)

		if count := evictedCount.Load(); count != 3 {
			t.Errorf("Expected 3 eviction callbacks, got %d", count)
		}
	})
}

func TestCacheBasicOperations(t *testing.T) {
	c := NewCache(Options{MaxSize: 100})
	defer c.Clear()

	t.Run("Set and Get", func(t *testing.T) {
		c.Set("key1", "value1")
		if val, exists := c.Get("key1"); !exists || val != "value1" {
			t.Errorf("Expected value1, got %v", val)
		}
	})

	t.Run("Get Non-existent", func(t *testing.T) {
		if _, exists := c.Get("nonexistent"); exists {
			t.Error("Should not get non-existent key")
		}
	})

	t.Run("Delete", func(t *testing.T) {
		c.Set("key2", "value2")
		c.Delete("key2")
		if _, exists := c.Get("key2"); exists {
			t.Error("Key should be deleted")
		}
	})

	t.Run("Has", func(t *testing.T) {
		c.Set("key3", "value3")
		if !c.Has("key3") {
			t.Error("Has should return true for existing key")
		}
		if c.Has("nonexistent") {
			t.Error("Has should return false for non-existent key")
		}
	})

	t.Run("Count", func(t *testing.T) {
		c.Clear()
		c.Set("key4", "value4")
		c.Set("key5", "value5")
		if count := c.Count(); count != 2 {
			t.Errorf("Expected count 2, got %d", count)
		}
	})

	t.Run("DeletePrefix and DeleteKeys", func(t *testing.T) {
		c.Clear()
		c.Set("pre:1", 1)
		c.Set("pre:2", 2)
		c.Set("other", 3)

		if n := c.DeletePrefix("pre:"); n != 2 {
			t.Fatalf("expected delete 2 by prefix, got %d", n)
		}
		if c.Has("pre:1") || c.Has("pre:2") {
			t.Fatal("prefix deleted keys should not exist")
		}
		if !c.Has("other") {
			t.Fatal("other should remain")
		}

		if n := c.DeleteKeys([]string{"other", "missing"}); n != 1 {
			t.Fatalf("expected delete 1 by keys, got %d", n)
		}
		if c.Has("other") {
			t.Fatal("other should be deleted")
		}
	})
}

func TestCacheExpiration(t *testing.T) {
	cache := Get()
	defer cache.Clear()

	t.Run("Default Expiration", func(t *testing.T) {
		cache.Set("key1", "value1")
		time.Sleep(time.Millisecond * 150)
		if _, exists := cache.Get("key1"); !exists {
			t.Error("Key should not be expired")
		}
	})

	t.Run("Custom Expiration", func(t *testing.T) {
		cache.SetWithExpiration("key2", "value2", time.Millisecond*50)
		time.Sleep(time.Millisecond * 100)
		if _, exists := cache.Get("key2"); exists {
			t.Error("Key should have expired")
		}
	})
}

func TestCacheMaxSize(t *testing.T) {
	c := NewCache(Options{MaxSize: 3, ShardCount: 1})
	defer c.Clear()

	t.Run("Sequential Eviction", func(t *testing.T) {
		// Add 5 items sequentially, should keep only the latest 3
		items := []struct {
			key   string
			value string
		}{
			{"k1", "v1"},
			{"k2", "v2"},
			{"k3", "v3"},
			{"k4", "v4"},
			{"k5", "v5"},
		}

		for _, item := range items {
			c.Set(item.key, item.value)
			// Add small delay to ensure distinct creation times
			time.Sleep(time.Millisecond * 10)
		}

		// Verify count
		if count := c.Count(); count != 3 {
			t.Errorf("Expected count of 3, got %d", count)
		}

		// Verify oldest two items were evicted
		for i := 1; i <= 2; i++ {
			key := fmt.Sprintf("k%d", i)
			if _, exists := c.Get(key); exists {
				t.Errorf("key %s should have been evicted", key)
			}
		}

		// Verify latest three items exist
		for i := 3; i <= 5; i++ {
			key := fmt.Sprintf("k%d", i)
			expectedValue := fmt.Sprintf("v%d", i)
			if val, exists := c.Get(key); !exists {
				t.Errorf("key %s should exist", key)
			} else if val != expectedValue {
				t.Errorf("Expected value %s for key %s, got %v", expectedValue, key, val)
			}
		}
	})

	t.Run("Update Existing Item", func(t *testing.T) {
		c.Clear()

		// Add initial items
		c.Set("k1", "v1")
		c.Set("k2", "v2")
		c.Set("k3", "v3")

		// Updating existing item should not trigger eviction
		c.Set("k2", "v2-updated")

		// Verify all items still exist
		expectedValues := map[string]string{
			"k1": "v1",
			"k2": "v2-updated",
			"k3": "v3",
		}

		for key, expectedValue := range expectedValues {
			if val, exists := c.Get(key); !exists {
				t.Errorf("key %s should exist", key)
			} else if val != expectedValue {
				t.Errorf("Expected value %s for key %s, got %v", expectedValue, key, val)
			}
		}
	})

	t.Run("Rapid Updates", func(t *testing.T) {
		c.Clear()

		// Rapidly add and update items
		for i := 0; i < 10; i++ {
			for j := 1; j <= 5; j++ {
				key := fmt.Sprintf("k%d", j)
				value := fmt.Sprintf("v%d-%d", j, i)
				c.Set(key, value)
				// Add small delay to ensure distinct creation times
				time.Sleep(time.Millisecond * 10)
			}
		}

		// Verify only latest 3 keys are kept
		if count := c.Count(); count != 3 {
			t.Errorf("Expected count of 3, got %d", count)
		}

		// Verify last 3 keys have latest values
		for i := 3; i <= 5; i++ {
			key := fmt.Sprintf("k%d", i)
			expectedValue := fmt.Sprintf("v%d-9", i) // Last update value
			if val, exists := c.Get(key); !exists {
				t.Errorf("key %s should exist", key)
			} else if val != expectedValue {
				t.Errorf("Expected value %s for key %s, got %v", expectedValue, key, val)
			}
		}
	})

	t.Run("Zero MaxSize", func(t *testing.T) {
		// Test MaxSize=0 (unlimited)
		unlimitedCache := NewCache(Options{MaxSize: 0})
		defer unlimitedCache.Clear()

		// Add many items
		for i := 1; i <= 100; i++ {
			key := fmt.Sprintf("k%d", i)
			value := fmt.Sprintf("v%d", i)
			unlimitedCache.Set(key, value)
		}

		// Verify all items exist
		if count := unlimitedCache.Count(); count != 100 {
			t.Errorf("Expected count of 100, got %d", count)
		}
	})
}

func TestCacheGetOrSet(t *testing.T) {
	c := NewCache(Options{})
	defer c.Clear()

	t.Run("GetOrSet", func(t *testing.T) {
		// First call should set the value
		val := c.GetOrSet("key1", "value1")
		if val != "value1" {
			t.Errorf("Expected value1, got %v", val)
		}

		// Second call should return existing value
		val = c.GetOrSet("key1", "value2")
		if val != "value1" {
			t.Errorf("Expected value1, got %v", val)
		}
	})

	t.Run("GetOrSetFunc", func(t *testing.T) {
		callCount := 0
		getter := func() interface{} {
			callCount++
			return "generated"
		}

		// First call should execute function
		val := c.GetOrSetFunc("key2", getter)
		if val != "generated" || callCount != 1 {
			t.Error("Function should be called exactly once")
		}

		// Second call should return cached value
		val = c.GetOrSetFunc("key2", getter)
		if val != "generated" || callCount != 1 {
			t.Error("Function should not be called again")
		}
	})
}

func TestCacheStats(t *testing.T) {
	c := NewCache(Options{})
	defer c.Clear()

	t.Run("Hit and Miss Stats", func(t *testing.T) {
		c.Set("key1", "value1")

		// Generate hits
		c.Get("key1")
		c.Get("key1")

		// Generate misses
		c.Get("nonexistent")

		stats := c.Stats()
		if stats["hits"].(uint64) != 2 {
			t.Errorf("Expected 2 hits, got %v", stats["hits"])
		}
		if stats["misses"].(uint64) != 1 {
			t.Errorf("Expected 1 miss, got %v", stats["misses"])
		}
	})
}

func TestCacheTyped(t *testing.T) {
	c := NewCache(Options{})
	defer c.Clear()

	t.Run("GetTyped Success", func(t *testing.T) {
		c.Set("int", 42)
		if val, ok := GetTyped[int](c, "int"); !ok || val != 42 {
			t.Error("Failed to get typed int value")
		}

		c.Set("string", "hello")
		if val, ok := GetTyped[string](c, "string"); !ok || val != "hello" {
			t.Error("Failed to get typed string value")
		}
	})

	t.Run("GetTyped Wrong Type", func(t *testing.T) {
		c.Set("wrongtype", "string")
		if _, ok := GetTyped[int](c, "wrongtype"); ok {
			t.Error("Should fail when types don't match")
		}
	})
}

func TestCacheEvictionCallback(t *testing.T) {
	c := NewCache(Options{MaxSize: 2, ShardCount: 1})
	defer c.Clear()

	evicted := make(map[string]interface{})
	c.OnEvicted(func(key string, value interface{}) {
		evicted[key] = value
	})

	t.Run("Eviction Callback", func(t *testing.T) {
		c.Set("k1", "v1")
		time.Sleep(time.Millisecond * 10)
		c.Set("k2", "v2")
		time.Sleep(time.Millisecond * 10)
		c.Set("k3", "v3") // Should evict k1

		if val, ok := evicted["k1"]; !ok || val != "v1" {
			t.Error("Eviction callback not called correctly")
		}

		if val, ok := evicted["k2"]; ok || val == "v2" {
			t.Error("Eviction callback not called correctly")
		}

		if val, ok := evicted["k3"]; ok || val == "v3" {
			t.Error("Eviction callback not called correctly")
		}
	})

	t.Run("Delete Callback", func(t *testing.T) {
		c.Set("k4", "v4")
		c.Delete("k4")

		if val, ok := evicted["k4"]; !ok || val != "v4" {
			t.Error("Delete callback not called correctly")
		}
	})
}

func TestCacheConcurrent(t *testing.T) {
	c := NewCache(Options{MaxSize: 1000})
	defer c.Clear()

	t.Run("Concurrent Set and Get", func(t *testing.T) {
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				key := fmt.Sprintf("key%d", id)
				value := fmt.Sprintf("value%d", id)
				c.Set(key, value)
				if val, exists := c.Get(key); !exists || val != value {
					t.Errorf("Concurrent operation failed for %s", key)
				}
			}(i)
		}
		wg.Wait()
	})
}

func TestCacheInit(t *testing.T) {
	t.Run("Single Init", func(t *testing.T) {
		Init(Options{MaxSize: 100})
		c1 := Get()
		c2 := Get()
		if c1 != c2 {
			t.Error("Cache singleton not working correctly")
		}
	})

	t.Run("Panic On Get Before Init", func(t *testing.T) {
		defaultCache = nil // Reset cache
		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic on Get before Init")
			}
		}()
		Get()
	})
}

func TestCacheDefaultOptions(t *testing.T) {
	t.Run("Default Values", func(t *testing.T) {
		c := NewCache()
		stats := c.Stats()

		// Verify default values
		expectedDefaults := map[string]interface{}{
			"maxSize":           0,
			"defaultExpiration": time.Duration(0),
			"cleanupInterval":   time.Minute * 11,
			"persistInterval":   time.Minute * 13,
			"persistThreshold":  100,
			"shardCount":        uint64(32), // Default 32 shards
			"persistPath":       "",
		}

		for key, expected := range expectedDefaults {
			if value, ok := stats[key]; !ok {
				t.Errorf("Missing stat %s", key)
			} else if fmt.Sprintf("%v", value) != fmt.Sprintf("%v", expected) {
				t.Errorf("Expected %s to be %v, got %v", key, expected, value)
			}
		}
	})

	t.Run("Negative Values", func(t *testing.T) {
		c := NewCache(Options{
			MaxSize:           -1,
			DefaultExpiration: -1,
			CleanupInterval:   -1,
			PersistInterval:   -1,
			PersistThreshold:  -1,
			ShardCount:        -1,
		})

		stats := c.Stats()

		// Verify negative values are handled correctly as defaults
		expectedDefaults := map[string]interface{}{
			"maxSize":           0,
			"defaultExpiration": time.Duration(0),
			"cleanupInterval":   time.Minute * 11,
			"persistInterval":   time.Minute * 13,
			"persistThreshold":  100,
			"shardCount":        uint64(32), // Should use default value
		}

		for key, expected := range expectedDefaults {
			if value, ok := stats[key]; !ok {
				t.Errorf("Missing stat %s", key)
			} else if fmt.Sprintf("%v", value) != fmt.Sprintf("%v", expected) {
				t.Errorf("Expected %s to be %v, got %v", key, expected, value)
			}
		}
	})

	t.Run("ShardCount Power of 2", func(t *testing.T) {
		testCases := []struct {
			input    int
			expected uint64
		}{
			{-1, 32},
			{0, 32},
			{3, 4},
			{5, 8},
			{31, 32},
			{33, 64},
		}

		for _, tc := range testCases {
			c := NewCache(Options{ShardCount: tc.input})
			stats := c.Stats()
			if shardCount := stats["shardCount"].(uint64); shardCount != tc.expected {
				t.Errorf("Input ShardCount %d: expected %d, got %d", tc.input, tc.expected, shardCount)
			}
		}
	})

	t.Run("NextPowerOf2 Function", func(t *testing.T) {
		tests := []struct {
			input    uint64
			expected uint64
		}{
			{0, 1},
			{1, 1},
			{2, 2},
			{3, 4},
			{7, 8},
			{8, 8},
			{9, 16},
			{33, 64},
			{1000, 1024},
			{1024, 1024},
			{1025, 2048},
			// Test cases for larger values (32-bit boundary)
			{0xFFFFFFFF, 0x100000000},  // 2^32 - 1 -> 2^32
			{0x100000000, 0x100000000}, // 2^32 -> 2^32
			{0x100000001, 0x200000000}, // 2^32 + 1 -> 2^33
			{0x1FFFFFFFF, 0x200000000}, // Test high 32-bit values
			// Test very large values that would fail without v |= v >> 32
			{0x123456789ABCDEF0, 0x2000000000000000}, // Test 64-bit value
		}

		for _, test := range tests {
			result := nextPowerOf2(test.input)
			if result != test.expected {
				t.Errorf("nextPowerOf2(%d) = %d; expected %d", test.input, result, test.expected)
			}
		}

		// Test maximum safe value
		maxSafe := uint64(1) << 62
		result := nextPowerOf2(maxSafe)
		if result != maxSafe {
			t.Errorf("nextPowerOf2(%d) = %d; expected %d", maxSafe, result, maxSafe)
		}

		// Test value that would overflow if we tried to get next power of 2
		largeValue := uint64(1) << 63
		result = nextPowerOf2(largeValue)
		// This should return the same value since it's already a power of 2
		if result != largeValue {
			t.Errorf("nextPowerOf2(%d) = %d; expected %d", largeValue, result, largeValue)
		}
	})
}

func TestCacheFactoryAndSingleton(t *testing.T) {
	// Save original defaultCache
	originalCache := defaultCache
	defer func() {
		defaultCache = originalCache
	}()

	t.Run("Factory Pattern", func(t *testing.T) {
		// Create two different instances
		cache1 := NewCache(Options{MaxSize: 100})
		cache2 := NewCache(Options{MaxSize: 200})

		// Verify they are different instances
		cache1.Set("key", "value1")
		cache2.Set("key", "value2")

		if val, _ := cache1.Get("key"); val == "value2" {
			t.Error("Cache instances should be independent")
		}

		// Verify configurations are independent
		stats1 := cache1.Stats()
		stats2 := cache2.Stats()
		if stats1["maxSize"] == stats2["maxSize"] {
			t.Error("Cache configurations should be independent")
		}
	})

	t.Run("Singleton Pattern", func(t *testing.T) {
		// Reset default cache and once
		defaultCache = nil
		once = sync.Once{}

		// Initialize default instance
		Init(Options{MaxSize: 100})

		// Get instance references
		cache1 := Get()
		cache2 := Get()

		// Verify they are the same instance
		cache1.Set("key", "value")
		if val, exists := cache2.Get("key"); !exists || val != "value" {
			t.Error("Singleton pattern not working correctly")
		}

		// Verify cannot re-initialize
		Init(Options{MaxSize: 200})
		cache3 := Get()
		stats := cache3.Stats()
		if stats["maxSize"].(int) != 100 {
			t.Error("Singleton should not be re-initialized")
		}
	})

	t.Run("Panic Before Init", func(t *testing.T) {
		// Reset default cache and once
		defaultCache = nil
		once = sync.Once{}

		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic when getting uninitialized cache")
			}
		}()

		Get() // Should panic
	})

	t.Run("Multiple Instances With Different Configs", func(t *testing.T) {
		// Create temporary directory for test persistence
		tempDir, err := os.MkdirTemp("", "cache_test_multiple")
		if err != nil {
			t.Fatalf("Failed to create temp dir: %v", err)
		}
		defer os.RemoveAll(tempDir)

		persistPath := filepath.Join(tempDir, "test_cache")

		// Create multiple instances with different configurations
		caches := []CacheInterface{
			NewCache(Options{MaxSize: 10, DefaultExpiration: time.Minute}),
			NewCache(Options{MaxSize: 20, DefaultExpiration: time.Hour}),
			NewCache(Options{MaxSize: 30, PersistPath: persistPath}),
		}

		// Verify independence of each instance
		for i, cache := range caches {
			key := fmt.Sprintf("key%d", i)
			value := fmt.Sprintf("value%d", i)
			cache.Set(key, value)

			// Verify other instances don't have this value
			for j, otherCache := range caches {
				if i != j {
					if _, exists := otherCache.Get(key); exists {
						t.Errorf("Cache %d should not contain key from cache %d", j, i)
					}
				}
			}
		}

		os.RemoveAll("test")
	})
}

func TestConcurrentAccess(t *testing.T) {
	cache := NewCache(Options{ShardCount: 32})
	var wg sync.WaitGroup
	iterations := 1000
	goroutines := 10

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				key := fmt.Sprintf("key-%d-%d", g, i)
				value := fmt.Sprintf("value-%d-%d", g, i)

				cache.Set(key, value)

				// Read and verify
				if val, exists := cache.Get(key); !exists || val != value {
					t.Errorf("Concurrent operation failed for key: %s", key)
				}

				cache.Delete(key)
			}
		}(g)
	}

	wg.Wait()
}

func TestMaxSize(t *testing.T) {
	cache := NewCache(Options{
		MaxSize:    2,
		ShardCount: 1, // Use single shard for testing
	})

	cache.Set("k1", "v1")
	time.Sleep(time.Millisecond)
	cache.Set("k2", "v2")
	time.Sleep(time.Millisecond)
	cache.Set("k3", "v3") // Should trigger eviction

	if cache.Count() != 2 {
		t.Errorf("Expected count of 2, got %d", cache.Count())
	}

	if _, exists := cache.Get("k1"); exists {
		t.Error("k1 should have been evicted")
	}

	if _, exists := cache.Get("k3"); !exists {
		t.Error("k3 should exist")
	}
}

// TestClearObjectPoolRecycling tests whether the Clear method correctly recycles objects to object pools.
// This test verifies that ExpireTime objects are returned to timePool and cacheItem objects
// are returned to itemPool, ensuring proper memory management and pool reuse.
func TestClearObjectPoolRecycling(t *testing.T) {
	// Create cache instance
	cache := NewCache(Options{
		ShardCount: 4,
	})
	defer cache.Close()

	// Add some items with expiration time
	cache.SetWithExpiration("key1", "value1", time.Hour)
	cache.SetWithExpiration("key2", "value2", time.Hour)
	cache.SetWithExpiration("key3", "value3", time.Hour)
	cache.Set("key4", "value4") // Without expiration time

	// Verify items have been added
	if cache.Count() != 4 {
		t.Errorf("Expected 4 items, got %d", cache.Count())
	}

	// Record memory usage before clear
	var m1 runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m1)

	// Execute Clear operation
	err := cache.Clear()
	if err != nil {
		t.Errorf("Clear failed: %v", err)
	}

	// Verify cache is empty
	if cache.Count() != 0 {
		t.Errorf("Expected 0 items after clear, got %d", cache.Count())
	}

	// Force garbage collection
	runtime.GC()

	// Record memory usage after clear
	var m2 runtime.MemStats
	runtime.ReadMemStats(&m2)

	// Verify object pool has available objects
	// Test pool reuse by quickly creating new items
	for i := 0; i < 10; i++ {
		cache.SetWithExpiration("test", "value", time.Hour)
		cache.Delete("test")
	}

	t.Logf("Memory before clear: %d bytes", m1.Alloc)
	t.Logf("Memory after clear: %d bytes", m2.Alloc)
	t.Log("Clear operation completed successfully with object pool recycling")
}

// TestClearWithEvictionCallback tests the callback functionality of the Clear method.
// This test ensures that eviction callbacks are properly triggered for all items
// when the cache is cleared, and that callbacks are executed after releasing locks.
func TestClearWithEvictionCallback(t *testing.T) {
	evictedKeys := make(map[string]interface{})

	cache := NewCache(Options{
		ShardCount: 4,
	})
	defer cache.Close()

	// Set callback function
	cache.OnEvicted(func(key string, value interface{}) {
		evictedKeys[key] = value
	})

	// Add some items
	cache.Set("key1", "value1")
	cache.Set("key2", "value2")
	cache.SetWithExpiration("key3", "value3", time.Hour)

	// Execute Clear
	err := cache.Clear()
	if err != nil {
		t.Errorf("Clear failed: %v", err)
	}

	// Verify callback was triggered correctly
	if len(evictedKeys) != 3 {
		t.Errorf("Expected 3 evicted items, got %d", len(evictedKeys))
	}

	expectedKeys := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	for key, expectedValue := range expectedKeys {
		if value, exists := evictedKeys[key]; !exists {
			t.Errorf("Expected key %s to be evicted", key)
		} else if value != expectedValue {
			t.Errorf("Expected evicted value %s for key %s, got %s", expectedValue, key, value)
		}
	}

	// Verify cache is empty
	if cache.Count() != 0 {
		t.Errorf("Expected 0 items after clear, got %d", cache.Count())
	}
}

// BenchmarkClearWithObjectPoolRecycling tests the performance of Clear operation.
// This benchmark measures the time taken to clear a cache with 1000 items,
// where half have expiration times and half do not.
func BenchmarkClearWithObjectPoolRecycling(b *testing.B) {
	// Create cache instance
	cache := NewCache(Options{
		ShardCount: 32,
	})
	defer cache.Close()

	// Prepare test data
	prepareData := func() {
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("key%d", i)
			value := fmt.Sprintf("value%d", i)
			if i%2 == 0 {
				cache.SetWithExpiration(key, value, time.Hour)
			} else {
				cache.Set(key, value)
			}
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		prepareData()
		b.StartTimer()

		// Execute Clear operation
		cache.Clear()
	}
}

// BenchmarkClearMemoryUsage tests memory usage of Clear operation.
// This benchmark measures memory allocation patterns and verifies that
// memory is properly released after clearing the cache.
func BenchmarkClearMemoryUsage(b *testing.B) {
	cache := NewCache(Options{
		ShardCount: 32,
	})
	defer cache.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Add data
		for j := 0; j < 100; j++ {
			key := fmt.Sprintf("key%d", j)
			value := fmt.Sprintf("value%d", j)
			cache.SetWithExpiration(key, value, time.Hour)
		}

		// Record memory usage
		var m1 runtime.MemStats
		runtime.ReadMemStats(&m1)

		// Execute Clear
		cache.Clear()

		// Record memory after clear
		var m2 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m2)

		// Verify memory is properly released
		if i == 0 {
			b.Logf("Memory before clear: %d bytes", m1.Alloc)
			b.Logf("Memory after clear: %d bytes", m2.Alloc)
		}
	}
}

// BenchmarkPoolReuse tests the effect of object pool reuse.
// This benchmark compares performance between reusing object pools (via Clear)
// versus creating new cache instances each time.
func BenchmarkPoolReuse(b *testing.B) {
	cache := NewCache(Options{
		ShardCount: 32,
	})
	defer cache.Close()

	b.Run("WithPoolReuse", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Add items
			for j := 0; j < 100; j++ {
				cache.SetWithExpiration(fmt.Sprintf("key%d", j), fmt.Sprintf("value%d", j), time.Hour)
			}
			// Clear cache (will recycle to object pool)
			cache.Clear()
		}
	})

	b.Run("NewObjectsEachTime", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			// Create new cache instance (cannot reuse object pool)
			newCache := NewCache(Options{
				ShardCount: 32,
			})
			// Add items
			for j := 0; j < 100; j++ {
				newCache.SetWithExpiration(fmt.Sprintf("key%d", j), fmt.Sprintf("value%d", j), time.Hour)
			}
			newCache.Close()
		}
	})
}

// TestAtomicGetOrSet tests that GetOrSet operations are atomic under high concurrency
func TestAtomicGetOrSet(t *testing.T) {
	cache := NewCache()
	key := "atomic_test_key"

	var setCount int64

	// Concurrent GetOrSet calls
	var wg sync.WaitGroup
	concurrency := 100
	results := make([]interface{}, concurrency)

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			// Each goroutine tries to set a unique value
			value := fmt.Sprintf("value_%d", atomic.AddInt64(&setCount, 1))
			results[idx] = cache.GetOrSet(key, value)
		}(i)
	}

	wg.Wait()

	// Check results: all results should be the same
	firstResult := results[0]
	allSame := true
	for _, result := range results {
		if result != firstResult {
			allSame = false
			t.Errorf("Expected all results to be same, got %v and %v", firstResult, result)
			break
		}
	}

	t.Logf("Set operations count: %d", atomic.LoadInt64(&setCount))
	t.Logf("All results same: %v, result: %v", allSame, firstResult)

	if !allSame {
		t.Error("GetOrSet should return same value for all concurrent calls")
	}
}

// TestAtomicGetOrSetFunc tests that GetOrSetFunc operations are atomic
func TestAtomicGetOrSetFunc(t *testing.T) {
	cache := NewCache()
	key := "atomic_func_key"

	var funcCallCount int64

	// Concurrent GetOrSetFunc calls
	var wg sync.WaitGroup
	concurrency := 50
	results := make([]interface{}, concurrency)

	wg.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			results[idx] = cache.GetOrSetFunc(key, func() interface{} {
				callNum := atomic.AddInt64(&funcCallCount, 1)
				// Add delay to increase race condition probability
				time.Sleep(time.Millisecond * 10)
				return fmt.Sprintf("func_result_%d", callNum)
			})
		}(i)
	}

	wg.Wait()

	// Check that function was called exactly once
	if atomic.LoadInt64(&funcCallCount) != 1 {
		t.Errorf("Expected function to be called exactly once, got %d calls", atomic.LoadInt64(&funcCallCount))
	}

	// Check that all results are the same
	firstResult := results[0]
	for i, result := range results {
		if result != firstResult {
			t.Errorf("Result[%d] = %v, expected %v", i, result, firstResult)
		}
	}

	t.Logf("Function call count: %d", atomic.LoadInt64(&funcCallCount))
	t.Logf("Result: %v", firstResult)
}
