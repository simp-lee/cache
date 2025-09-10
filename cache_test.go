package shardedcache

import (
	"fmt"
	"math/rand"
	"os"
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

		// 立即获取应该存在
		if _, exists := cache.Get("key2"); !exists {
			t.Error("Key should exist immediately after setting")
		}

		// 等待过期
		time.Sleep(time.Millisecond * 200)

		if _, exists := cache.Get("key2"); exists {
			t.Error("Key should have expired")
		}
	})

	t.Run("GetOrSet", func(t *testing.T) {
		// 首次获取会设置值
		val := cache.GetOrSet("key3", "value3")
		if val != "value3" {
			t.Errorf("Expected value3, got %v", val)
		}

		// 再次获取应返回已存在的值
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

		// 首次调用
		val := cache.GetOrSetFunc("func_key", getValue)
		if val != "generated_value" {
			t.Errorf("Expected 'generated_value', got %v", val)
		}
		if callCount != 1 {
			t.Errorf("Function should be called exactly once, got %d calls", callCount)
		}

		// 第二次调用应该返回缓存的值
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
	// 初始化缓存配置
	opts := Options{
		MaxSize:           100,
		DefaultExpiration: time.Second * 5,
		CleanupInterval:   time.Second,
		ShardCount:        32, // 添加分片数量配置
	}

	// 初始化单例缓存
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

		// 立即获取应该存在
		if _, exists := cache.Get("key2"); !exists {
			t.Error("Key should exist immediately after setting")
		}

		// 等待过期
		time.Sleep(time.Millisecond * 200)

		if _, exists := cache.Get("key2"); exists {
			t.Error("Key should have expired")
		}
	})

	t.Run("GetOrSet", func(t *testing.T) {
		// 首次获取会设置值
		val := cache.GetOrSet("key3", "value3")
		if val != "value3" {
			t.Errorf("Expected value3, got %v", val)
		}

		// 再次获取应返回已存在的值
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
		cache.Clear() // 清空之前的数据
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
		// 验证分片数量
		if stats["shards"].(uint64) != 32 {
			t.Errorf("Incorrect shard count in stats: %v", stats["shards"])
		}
	})

	t.Run("MaxSize With Single Shard", func(t *testing.T) {
		// 使用单分片测试最大容量
		singleShardOpts := Options{
			MaxSize:    2,
			ShardCount: 1, // 使用单个分片便于测试淘汰
		}
		singleCache := NewCache(singleShardOpts)

		singleCache.Set("k1", "v1")
		time.Sleep(time.Millisecond * 10)
		singleCache.Set("k2", "v2")
		time.Sleep(time.Millisecond * 10)
		singleCache.Set("k3", "v3") // 应该淘汰最旧的项

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
		// 验证单例模式
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
		// 首次调用，应该执行函数
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

		// 第二次调用，应该返回缓存的值而不执行函数
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

		// 首次调用
		val := cache.GetOrSetFuncWithExpiration("exp_key", getValue, time.Millisecond*100)
		if val != "expiring_value" {
			t.Errorf("Expected 'expiring_value', got %v", val)
		}
		if atomic.LoadInt32(&callCount) != 1 {
			t.Errorf("Function should be called exactly once, got %d calls", callCount)
		}

		// 立即获取，应该返回缓存值
		val = cache.GetOrSetFuncWithExpiration("exp_key", getValue, time.Millisecond*100)
		if val != "expiring_value" {
			t.Errorf("Expected cached 'expiring_value', got %v", val)
		}
		if atomic.LoadInt32(&callCount) != 1 {
			t.Errorf("Function should not be called again, got %d calls", callCount)
		}

		// 等待过期
		time.Sleep(time.Millisecond * 200)

		// 过期后再次调用，应该重新生成值
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
			// 增加一些随机延迟，使竞争条件更容易出现
			time.Sleep(time.Duration(rand.Intn(10)) * time.Millisecond)
			atomic.AddInt32(&callCount, 1)
			return "concurrent_value"
		}

		var wg sync.WaitGroup
		concurrency := 100 // 增加并发数以更好地测试
		wg.Add(concurrency)

		// 重置计数器
		atomic.StoreInt32(&callCount, 0)

		// 并发访问
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

		// 验证函数只被调用一次
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

		// 第一轮并发访问
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

		// 等待过期
		time.Sleep(time.Millisecond * 100)

		// 第二轮并发访问
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
		CleanupInterval: time.Millisecond * 100, // 设置较小的清理间隔
		ShardCount:      1,                      // 使用单分片便于测试淘汰
	})

	t.Run("Eviction Callback", func(t *testing.T) {
		var mu sync.Mutex
		evictedKeys := make([]interface{}, 0)
		evictedVals := make([]interface{}, 0)

		// 设置回调函数
		cache.OnEvicted(func(key string, value interface{}) {
			mu.Lock()
			evictedKeys = append(evictedKeys, key)
			evictedVals = append(evictedVals, value)
			mu.Unlock()
		})

		// 添加数据触发淘汰
		cache.Set("k1", "v1")
		time.Sleep(time.Millisecond * 10)
		cache.Set("k2", "v2")
		time.Sleep(time.Millisecond * 10)
		cache.Set("k3", "v3") // 应该触发 k1 的淘汰

		// 等待一段时间确保回调执行完成
		time.Sleep(time.Millisecond * 50)

		mu.Lock()
		defer mu.Unlock()

		// 验证回调是否正确执行
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

		// 设置快过期的项
		cache.SetWithExpiration("exp_key", "exp_val", time.Millisecond*50)

		// 手动触发过期
		time.Sleep(time.Millisecond * 100)
		cache.Get("exp_key") // 触发过期检查

		// 等待回调
		select {
		case <-evicted:
			// 回调被正确触发
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

		// 等待一段时间确保回调执行完成
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

		// 添加多个项
		cache.Set("clear1", "val1")
		cache.Set("clear2", "val2")
		cache.Set("clear3", "val3")

		// 清空缓存
		cache.Clear()

		// 等待一段时间确保回调执行完成
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
		// 按顺序添加5个项目，应该只保留最新的3个
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
			// 添加小延时确保创建时间有明显差异
			time.Sleep(time.Millisecond * 10)
		}

		// 验证数量
		if count := c.Count(); count != 3 {
			t.Errorf("Expected count of 3, got %d", count)
		}

		// 验证最旧的两个项目被淘汰
		for i := 1; i <= 2; i++ {
			key := fmt.Sprintf("k%d", i)
			if _, exists := c.Get(key); exists {
				t.Errorf("key %s should have been evicted", key)
			}
		}

		// 验证最新的三个项目存在
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

		// 添加初始项目
		c.Set("k1", "v1")
		c.Set("k2", "v2")
		c.Set("k3", "v3")

		// 更新现有项目不应触发淘汰
		c.Set("k2", "v2-updated")

		// 验证所有项目仍然存在
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

		// 快速添加和更新项目
		for i := 0; i < 10; i++ {
			for j := 1; j <= 5; j++ {
				key := fmt.Sprintf("k%d", j)
				value := fmt.Sprintf("v%d-%d", j, i)
				c.Set(key, value)
				// 添加小延时确保创建时间有明显差异
				time.Sleep(time.Millisecond * 10)
			}
		}

		// 验证只保留了最新的3个键
		if count := c.Count(); count != 3 {
			t.Errorf("Expected count of 3, got %d", count)
		}

		// 验证最后3个键的值是最新的
		for i := 3; i <= 5; i++ {
			key := fmt.Sprintf("k%d", i)
			expectedValue := fmt.Sprintf("v%d-9", i) // 最后一次更新的值
			if val, exists := c.Get(key); !exists {
				t.Errorf("key %s should exist", key)
			} else if val != expectedValue {
				t.Errorf("Expected value %s for key %s, got %v", expectedValue, key, val)
			}
		}
	})

	t.Run("Zero MaxSize", func(t *testing.T) {
		// 测试MaxSize为0的情况（无限制）
		unlimitedCache := NewCache(Options{MaxSize: 0})
		defer unlimitedCache.Clear()

		// 添加大量项目
		for i := 1; i <= 100; i++ {
			key := fmt.Sprintf("k%d", i)
			value := fmt.Sprintf("v%d", i)
			unlimitedCache.Set(key, value)
		}

		// 验证所有项目都存在
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

		// 验证默认值
		expectedDefaults := map[string]interface{}{
			"maxSize":           0,
			"defaultExpiration": time.Duration(0),
			"cleanupInterval":   time.Minute * 11,
			"persistInterval":   time.Minute * 13,
			"persistThreshold":  100,
			"shardCount":        uint64(32), // 默认32个分片
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

		// 验证负值被正确处理为默认值
		expectedDefaults := map[string]interface{}{
			"maxSize":           0,
			"defaultExpiration": time.Duration(0),
			"cleanupInterval":   time.Minute * 11,
			"persistInterval":   time.Minute * 13,
			"persistThreshold":  100,
			"shardCount":        uint64(32), // 应该使用默认值
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
	// 保存原始的 defaultCache
	originalCache := defaultCache
	defer func() {
		defaultCache = originalCache
	}()

	t.Run("Factory Pattern", func(t *testing.T) {
		// 创建两个不同的实例
		cache1 := NewCache(Options{MaxSize: 100})
		cache2 := NewCache(Options{MaxSize: 200})

		// 验证它们是不同的实例
		cache1.Set("key", "value1")
		cache2.Set("key", "value2")

		if val, _ := cache1.Get("key"); val == "value2" {
			t.Error("Cache instances should be independent")
		}

		// 验证配置是独立的
		stats1 := cache1.Stats()
		stats2 := cache2.Stats()
		if stats1["maxSize"] == stats2["maxSize"] {
			t.Error("Cache configurations should be independent")
		}
	})

	t.Run("Singleton Pattern", func(t *testing.T) {
		// 重置默认缓存和 once
		defaultCache = nil
		once = sync.Once{}

		// 初始化默认实例
		Init(Options{MaxSize: 100})

		// 获取实例引用
		cache1 := Get()
		cache2 := Get()

		// 验证是同一个实例
		cache1.Set("key", "value")
		if val, exists := cache2.Get("key"); !exists || val != "value" {
			t.Error("Singleton pattern not working correctly")
		}

		// 验证无法重新初始化
		Init(Options{MaxSize: 200})
		cache3 := Get()
		stats := cache3.Stats()
		if stats["maxSize"].(int) != 100 {
			t.Error("Singleton should not be re-initialized")
		}
	})

	t.Run("Panic Before Init", func(t *testing.T) {
		// 重置默认缓存和 once
		defaultCache = nil
		once = sync.Once{}

		defer func() {
			if r := recover(); r == nil {
				t.Error("Expected panic when getting uninitialized cache")
			}
		}()

		Get() // 应该 panic
	})

	t.Run("Multiple Instances With Different Configs", func(t *testing.T) {
		// 创建具有不同配置的多个实例
		caches := []CacheInterface{
			NewCache(Options{MaxSize: 10, DefaultExpiration: time.Minute}),
			NewCache(Options{MaxSize: 20, DefaultExpiration: time.Hour}),
			NewCache(Options{MaxSize: 30, PersistPath: "test"}),
		}

		// 验证每个实例的独立性
		for i, cache := range caches {
			key := fmt.Sprintf("key%d", i)
			value := fmt.Sprintf("value%d", i)
			cache.Set(key, value)

			// 验证其他实例没有这个值
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

				// 写入
				cache.Set(key, value)

				// 读取并验证
				if val, exists := cache.Get(key); !exists || val != value {
					t.Errorf("Concurrent operation failed for key: %s", key)
				}

				// 删除
				cache.Delete(key)
			}
		}(g)
	}

	wg.Wait()
}

func TestMaxSize(t *testing.T) {
	cache := NewCache(Options{
		MaxSize:    2,
		ShardCount: 1, // 使用单个分片便于测试
	})

	cache.Set("k1", "v1")
	time.Sleep(time.Millisecond)
	cache.Set("k2", "v2")
	time.Sleep(time.Millisecond)
	cache.Set("k3", "v3") // 应该触发淘汰

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
