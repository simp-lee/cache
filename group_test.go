package shardedcache

import (
	"testing"
	"time"
)

func TestCacheGroup(t *testing.T) {
	cache := NewCache(Options{})

	t.Run("Group Operations", func(t *testing.T) {
		users := cache.Group("users")
		posts := cache.Group("posts")

		// 基本操作测试
		users.Set("1", "user1")
		users.Set("2", "user2")
		posts.Set("1", "post1")

		// 获取值
		if val, exists := users.Get("1"); !exists || val != "user1" {
			t.Error("Failed to get value from group")
		}

		// 获取组内所有键
		userKeys := users.Keys()
		if len(userKeys) != 2 {
			t.Errorf("Expected 2 keys in users group, got %d", len(userKeys))
		}

		// 删除组内特定键
		users.Delete("1")
		if _, exists := users.Get("1"); exists {
			t.Error("Failed to delete key from group")
		}

		// 测试 Has 方法
		if !users.Has("2") {
			t.Error("Has should return true for existing key")
		}
		if users.Has("1") {
			t.Error("Has should return false for deleted key")
		}
		if users.Has("nonexistent") {
			t.Error("Has should return false for nonexistent key")
		}

		// 清空整个组
		users.Clear()
		if len(users.Keys()) != 0 {
			t.Error("Failed to clear group")
		}

		// 验证其他组不受影响
		if _, exists := posts.Get("1"); !exists {
			t.Error("Other group should not be affected")
		}
	})

	t.Run("Group Expiration", func(t *testing.T) {
		users := cache.Group("users")
		users.SetWithExpiration("temp", "value", time.Millisecond*100)

		// 立即获取
		if _, exists := users.Get("temp"); !exists {
			t.Error("Value should exist before expiration")
		}

		// 等待过期
		time.Sleep(time.Millisecond * 200)

		if _, exists := users.Get("temp"); exists {
			t.Error("Value should have expired")
		}
	})
}

func TestCacheGroupSeparator(t *testing.T) {
	cache := NewCache(Options{})
	users := cache.Group("users")

	t.Run("Separator Conflict", func(t *testing.T) {
		// 直接设置带冒号的键
		cache.Set("users:1", "direct-value")

		// 通过组设置键
		users.Set("1", "group-value")

		// 验证两个值是独立的
		if val, exists := cache.Get("users:1"); !exists || val != "direct-value" {
			t.Error("Direct key should maintain its value")
		}

		if val, exists := users.Get("1"); !exists || val != "group-value" {
			t.Error("Group key should maintain its value")
		}

		// 清空组不应影响直接设置的键
		users.Clear()
		if _, exists := cache.Get("users:1"); !exists {
			t.Error("Direct key should not be affected by group clear")
		}
	})
}

func TestCacheGroupGetOrSet(t *testing.T) {
	cache := NewCache(Options{})
	users := cache.Group("users")

	t.Run("GetOrSet", func(t *testing.T) {
		// 首次调用应该设置值
		val := users.GetOrSet("1", "user1")
		if val != "user1" {
			t.Errorf("Expected user1, got %v", val)
		}

		// 第二次调用应该返回已存在的值
		val = users.GetOrSet("1", "user1-new")
		if val != "user1" {
			t.Errorf("Expected user1, got %v", val)
		}
	})

	t.Run("GetOrSetFunc", func(t *testing.T) {
		called := false
		f := func() interface{} {
			called = true
			return "user2"
		}

		// 首次调用应该执行函数
		val := users.GetOrSetFunc("2", f)
		if !called {
			t.Error("Function should have been called")
		}
		if val != "user2" {
			t.Errorf("Expected user2, got %v", val)
		}

		// 第二次调用不应该执行函数
		called = false
		val = users.GetOrSetFunc("2", f)
		if called {
			t.Error("Function should not have been called")
		}
		if val != "user2" {
			t.Errorf("Expected user2, got %v", val)
		}
	})

	t.Run("GetOrSetFuncWithExpiration", func(t *testing.T) {
		called := false
		f := func() interface{} {
			called = true
			return "user3"
		}

		// 首次调用应该执行函数
		val := users.GetOrSetFuncWithExpiration("3", f, 100*time.Millisecond)
		if !called {
			t.Error("Function should have been called")
		}
		if val != "user3" {
			t.Errorf("Expected user3, got %v", val)
		}

		// 等待过期
		time.Sleep(200 * time.Millisecond)

		// 过期后的调用应该再次执行函数
		called = false
		val = users.GetOrSetFuncWithExpiration("3", f, 100*time.Millisecond)
		if !called {
			t.Error("Function should have been called after expiration")
		}
		if val != "user3" {
			t.Errorf("Expected user3, got %v", val)
		}
	})
}
