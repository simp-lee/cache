package shardedcache

import (
	"sync"
	"testing"
)

func TestDeletePrefixEdgeCases(t *testing.T) {
	cache := NewCache(Options{ShardCount: 4})
	defer cache.Clear()

	t.Run("Empty prefix should return 0", func(t *testing.T) {
		cache.Set("key1", "value1")
		cache.Set("key2", "value2")

		deleted := cache.DeletePrefix("")
		if deleted != 0 {
			t.Errorf("Expected 0 deletions for empty prefix, got %d", deleted)
		}

		// Keys should still exist
		if !cache.Has("key1") || !cache.Has("key2") {
			t.Error("Keys should not be deleted with empty prefix")
		}
	})

	t.Run("Non-existent prefix should return 0", func(t *testing.T) {
		cache.Clear()
		cache.Set("key1", "value1")

		deleted := cache.DeletePrefix("nonexistent:")
		if deleted != 0 {
			t.Errorf("Expected 0 deletions for non-existent prefix, got %d", deleted)
		}
	})

	t.Run("Concurrent prefix deletion", func(t *testing.T) {
		cache.Clear()

		// Set up test data with unique keys
		for i := 0; i < 50; i++ {
			cache.Set("prefix1:key"+string(rune('0'+i/10))+string(rune('0'+i%10)), i)
			cache.Set("prefix2:key"+string(rune('0'+i/10))+string(rune('0'+i%10)), i+50)
		}

		var wg sync.WaitGroup
		var total1, total2 int

		wg.Add(2)
		go func() {
			defer wg.Done()
			total1 = cache.DeletePrefix("prefix1:")
		}()
		go func() {
			defer wg.Done()
			total2 = cache.DeletePrefix("prefix2:")
		}()
		wg.Wait()

		// Each should delete their own prefix
		if total1 != 50 || total2 != 50 {
			t.Errorf("Expected 50 deletions each, got %d and %d", total1, total2)
		}
	})
}

func TestDeleteKeysEdgeCases(t *testing.T) {
	cache := NewCache(Options{ShardCount: 4})
	defer cache.Clear()

	t.Run("Empty keys slice should return 0", func(t *testing.T) {
		cache.Set("key1", "value1")

		deleted := cache.DeleteKeys([]string{})
		if deleted != 0 {
			t.Errorf("Expected 0 deletions for empty keys slice, got %d", deleted)
		}

		deleted = cache.DeleteKeys(nil)
		if deleted != 0 {
			t.Errorf("Expected 0 deletions for nil keys slice, got %d", deleted)
		}
	})

	t.Run("Mix of existing and non-existing keys", func(t *testing.T) {
		cache.Clear()
		cache.Set("key1", "value1")
		cache.Set("key2", "value2")

		deleted := cache.DeleteKeys([]string{"key1", "nonexistent", "key2", "another_missing"})
		if deleted != 2 {
			t.Errorf("Expected 2 deletions, got %d", deleted)
		}

		if cache.Has("key1") || cache.Has("key2") {
			t.Error("Existing keys should be deleted")
		}
	})

	t.Run("Duplicate keys in list", func(t *testing.T) {
		cache.Clear()
		cache.Set("key1", "value1")

		deleted := cache.DeleteKeys([]string{"key1", "key1", "key1"})
		if deleted != 1 {
			t.Errorf("Expected 1 deletion for duplicate keys, got %d", deleted)
		}
	})
}

func TestGroupDeleteEdgeCases(t *testing.T) {
	cache := NewCache(Options{ShardCount: 4})
	users := cache.Group("users")
	posts := cache.Group("posts")

	t.Run("Group empty prefix should return 0", func(t *testing.T) {
		users.Set("user1", "data1")
		posts.Set("post1", "data1")

		deleted := users.DeletePrefix("")
		if deleted != 0 {
			t.Errorf("Expected 0 deletions for empty prefix in group, got %d", deleted)
		}

		// Both groups should be unaffected
		if !users.Has("user1") || !posts.Has("post1") {
			t.Error("Keys should not be deleted with empty prefix in group")
		}
	})

	t.Run("Group isolation", func(t *testing.T) {
		cache.Clear()
		users.Set("item1", "user_data")
		posts.Set("item1", "post_data")

		deleted := users.DeletePrefix("item")
		if deleted != 1 {
			t.Errorf("Expected 1 deletion in users group, got %d", deleted)
		}

		if users.Has("item1") {
			t.Error("Users item1 should be deleted")
		}
		if !posts.Has("item1") {
			t.Error("Posts item1 should remain")
		}
	})

	t.Run("Group DeleteKeys with empty slice", func(t *testing.T) {
		deleted := users.DeleteKeys([]string{})
		if deleted != 0 {
			t.Errorf("Expected 0 deletions for empty keys in group, got %d", deleted)
		}

		deleted = users.DeleteKeys(nil)
		if deleted != 0 {
			t.Errorf("Expected 0 deletions for nil keys in group, got %d", deleted)
		}
	})
}
