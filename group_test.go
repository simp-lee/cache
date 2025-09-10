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

		// Basic operations test
		users.Set("1", "user1")
		users.Set("2", "user2")
		posts.Set("1", "post1")

		if val, exists := users.Get("1"); !exists || val != "user1" {
			t.Error("Failed to get value from group")
		}

		userKeys := users.Keys()
		if len(userKeys) != 2 {
			t.Errorf("Expected 2 keys in users group, got %d", len(userKeys))
		}

		users.Delete("1")
		if _, exists := users.Get("1"); exists {
			t.Error("Failed to delete key from group")
		}

		if !users.Has("2") {
			t.Error("Has should return true for existing key")
		}
		if users.Has("1") {
			t.Error("Has should return false for deleted key")
		}
		if users.Has("nonexistent") {
			t.Error("Has should return false for nonexistent key")
		}

		users.Clear()
		if len(users.Keys()) != 0 {
			t.Error("Failed to clear group")
		}

		// Verify other groups are not affected
		if _, exists := posts.Get("1"); !exists {
			t.Error("Other group should not be affected")
		}
	})

	t.Run("Group Expiration", func(t *testing.T) {
		users := cache.Group("users")
		users.SetWithExpiration("temp", "value", time.Millisecond*100)

		if _, exists := users.Get("temp"); !exists {
			t.Error("Value should exist before expiration")
		}

		time.Sleep(time.Millisecond * 200)

		if _, exists := users.Get("temp"); exists {
			t.Error("Value should have expired")
		}
	})

	t.Run("Group DeletePrefix and DeleteKeys", func(t *testing.T) {
		users := cache.Group("users")
		users.Set("a:1", 1)
		users.Set("a:2", 2)
		users.Set("b:1", 3)
		users.Set("c", 4)

		n := users.DeletePrefix("a:")
		if n != 2 {
			t.Fatalf("expected delete 2 by prefix, got %d", n)
		}
		if users.Has("a:1") || users.Has("a:2") {
			t.Fatal("prefix deleted keys should not exist")
		}
		if !users.Has("b:1") || !users.Has("c") {
			t.Fatal("other keys should remain")
		}

		n = users.DeleteKeys([]string{"b:1", "not-exist"})
		if n != 1 {
			t.Fatalf("expected delete 1 by keys, got %d", n)
		}
		if users.Has("b:1") {
			t.Fatal("b:1 should be deleted")
		}
		if !users.Has("c") {
			t.Fatal("c should remain")
		}

		users.Clear()
	})
}

func TestCacheGroupSeparator(t *testing.T) {
	cache := NewCache(Options{})
	users := cache.Group("users")

	t.Run("Separator Conflict", func(t *testing.T) {
		// Set key directly with colon
		cache.Set("users:1", "direct-value")

		// Set key through group
		users.Set("1", "group-value")

		// Verify the two values are independent
		if val, exists := cache.Get("users:1"); !exists || val != "direct-value" {
			t.Error("Direct key should maintain its value")
		}

		if val, exists := users.Get("1"); !exists || val != "group-value" {
			t.Error("Group key should maintain its value")
		}

		// Clearing group should not affect directly set keys
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
		// First call should set the value
		val := users.GetOrSet("1", "user1")
		if val != "user1" {
			t.Errorf("Expected user1, got %v", val)
		}

		// Second call should return existing value
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

		// First call should execute function
		val := users.GetOrSetFunc("2", f)
		if !called {
			t.Error("Function should have been called")
		}
		if val != "user2" {
			t.Errorf("Expected user2, got %v", val)
		}

		// Second call should not execute function
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

		// First call should execute function
		val := users.GetOrSetFuncWithExpiration("3", f, 100*time.Millisecond)
		if !called {
			t.Error("Function should have been called")
		}
		if val != "user3" {
			t.Errorf("Expected user3, got %v", val)
		}

		time.Sleep(200 * time.Millisecond)

		// Call after expiration should execute function again
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
