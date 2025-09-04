package shardedcache

import (
	"os"
	"testing"
	"time"
)

func TestCacheClose(t *testing.T) {
	t.Run("Close should not hang with cleaner enabled", func(t *testing.T) {
		// Create cache with short cleanup interval to ensure cleaner is running
		cache := NewCache(Options{
			CleanupInterval: time.Millisecond * 100, // Very short interval
		})

		// Set some data
		cache.Set("key1", "value1")
		cache.SetWithExpiration("key2", "value2", time.Millisecond*50) // Will expire quickly

		// Wait a bit to ensure cleaner has had a chance to run
		time.Sleep(time.Millisecond * 150)

		// Create a channel to detect if Close() hangs
		done := make(chan struct{})

		go func() {
			cache.Close()
			close(done)
		}()

		// Set a timeout - if Close() is working correctly, it should complete quickly
		select {
		case <-done:
			// Success! Close() completed
		case <-time.After(time.Second * 2):
			t.Fatal("Cache.Close() appeared to hang - cleaner goroutine likely not exiting")
		}
	})

	t.Run("Close without cleaner should work normally", func(t *testing.T) {
		// Create cache with no cleanup interval (cleaner disabled)
		cache := NewCache(Options{
			CleanupInterval: 0, // Disable cleaner
		})

		cache.Set("key1", "value1")

		// This should work fine even before the fix
		done := make(chan struct{})
		go func() {
			cache.Close()
			close(done)
		}()

		select {
		case <-done:
			// Success!
		case <-time.After(time.Second):
			t.Fatal("Cache.Close() hung even without cleaner")
		}
	})

	t.Run("Close with persistence should work", func(t *testing.T) {
		// Create cache with both cleaner and persistence enabled
		cache := NewCache(Options{
			CleanupInterval: time.Millisecond * 100,
			PersistPath:     "test_close_cache",
			PersistInterval: time.Millisecond * 200,
		})

		cache.Set("key1", "value1")

		// Wait a bit
		time.Sleep(time.Millisecond * 150)

		done := make(chan struct{})
		go func() {
			cache.Close()
			close(done)
		}()

		select {
		case <-done:
			// Success!
		case <-time.After(time.Second * 3):
			t.Fatal("Cache.Close() hung with persistence enabled")
		}

		// Clean up test files
		os.RemoveAll("test_close_cache")
	})
}
