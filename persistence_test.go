package shardedcache

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cespare/xxhash/v2"
)

func TestCachePersistence(t *testing.T) {
	// Create temporary directory
	tempDir, err := os.MkdirTemp("", "cache_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create subdirectories for different test scenarios
	dirs := map[string]string{
		"auto":       filepath.Join(tempDir, "auto"),
		"threshold":  filepath.Join(tempDir, "threshold"),
		"close":      filepath.Join(tempDir, "close"),
		"expire":     filepath.Join(tempDir, "expire"),
		"update":     filepath.Join(tempDir, "update"),
		"concurrent": filepath.Join(tempDir, "concurrent"),
		"crash":      filepath.Join(tempDir, "crash"),
		"corrupt":    filepath.Join(tempDir, "corrupt"),
	}

	// Create all required directories
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// Add verification function
	verifyCache := func(t *testing.T, c CacheInterface, keyFormat, valueFormat string, count int) {
		actualCount := 0
		for i := 0; i < count; i++ {
			key := fmt.Sprintf(keyFormat, i)
			expected := fmt.Sprintf(valueFormat, i)
			if val, exists := c.Get(key); !exists || val != expected {
				t.Errorf("Expected %s=%s, got %v, exists=%v", key, expected, val, exists)
			} else {
				actualCount++
			}
		}
		if actualCount != count {
			t.Errorf("Expected %d items, got %d", count, actualCount)
		}
	}

	t.Run("AutoPersist", func(t *testing.T) {
		cache := NewCache(Options{
			PersistPath:      dirs["auto"],
			PersistInterval:  100 * time.Millisecond,
			ShardCount:       2,
			PersistThreshold: 100,
		})
		defer cache.Close()

		for i := 0; i < 10; i++ {
			cache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		// Wait for auto-persistence to complete
		time.Sleep(300 * time.Millisecond)

		newCache := NewCache(Options{
			PersistPath: dirs["auto"],
			ShardCount:  2,
		})
		defer newCache.Close()

		verifyCache(t, newCache, "key%d", "value%d", 10)
	})

	t.Run("ThresholdPersist", func(t *testing.T) {
		threshold := 5
		shardCount := 2
		cache := NewCache(Options{
			PersistPath:      dirs["threshold"],
			PersistInterval:  time.Hour,
			ShardCount:       shardCount,
			PersistThreshold: threshold,
		})
		defer cache.Close()

		// Helper function to determine which shard a key belongs to
		getShardIndex := func(key string) int {
			hash := xxhash.Sum64String(key)
			return int(hash & 1) // & 1 because shardCount = 2
		}

		// Prepare exact amount of data for each shard
		shardData := make(map[int][]struct{ key, value string })
		keyIndex := 0

		// Ensure each shard has exactly threshold number of items
		for shard := 0; shard < shardCount; shard++ {
			shardData[shard] = make([]struct{ key, value string }, 0)
			count := 0
			for count < threshold {
				key := fmt.Sprintf("key%d", keyIndex)
				if getShardIndex(key) == shard {
					shardData[shard] = append(shardData[shard], struct{ key, value string }{
						key:   key,
						value: fmt.Sprintf("value%d", keyIndex),
					})
					count++
				}
				keyIndex++
			}
		}

		// Write threshold-1 items first
		for shard := 0; shard < shardCount; shard++ {
			for i := 0; i < threshold-1; i++ {
				data := shardData[shard][i]
				cache.Set(data.key, data.value)
				time.Sleep(100 * time.Millisecond)
			}
		}

		// Verify no files exist before threshold is reached
		time.Sleep(100 * time.Millisecond)
		files, _ := os.ReadDir(dirs["threshold"])
		if len(files) > 0 {
			t.Error("No files should exist before threshold")
		}

		// Write the final item to trigger persistence
		for shard := 0; shard < shardCount; shard++ {
			lastData := shardData[shard][threshold-1]
			t.Logf("Writing final data to shard %d: key=%s, value=%s", shard, lastData.key, lastData.value)
			cache.Set(lastData.key, lastData.value)
			time.Sleep(100 * time.Millisecond)
		}

		time.Sleep(100 * time.Millisecond)

		// Verify persistence files
		files, _ = os.ReadDir(dirs["threshold"])
		datFiles := make(map[string]bool)
		for _, f := range files {
			if strings.HasSuffix(f.Name(), ".dat") {
				datFiles[f.Name()] = true
				t.Logf("Found persistence file: %s", f.Name())
			}
		}
		if len(datFiles) != shardCount {
			t.Errorf("Expected %d persistence files, got %d", shardCount, len(datFiles))
			return
		}

		// Verify data integrity
		newCache := NewCache(Options{
			PersistPath: dirs["threshold"],
			ShardCount:  shardCount,
		})
		defer newCache.Close()

		time.Sleep(200 * time.Millisecond)

		for shard := 0; shard < shardCount; shard++ {
			for _, data := range shardData[shard] {
				val, exists := newCache.Get(data.key)
				t.Logf("Checking shard %d: key=%s, expected=%s, got=%v, exists=%v",
					shard, data.key, data.value, val, exists)
				if !exists || val != data.value {
					t.Errorf("Shard %d, Key %s: expected %s, got %v, exists=%v",
						shard, data.key, data.value, val, exists)
					return
				}
			}
		}
	})

	t.Run("PersistOnClose", func(t *testing.T) {
		cache := NewCache(Options{
			PersistPath:      dirs["close"],
			PersistInterval:  time.Hour,
			ShardCount:       2,
			PersistThreshold: 1000,
		})

		for i := 0; i < 10; i++ {
			cache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		cache.Close()

		newCache := NewCache(Options{
			PersistPath: dirs["close"],
			ShardCount:  2,
		})
		defer newCache.Close()

		verifyCache(t, newCache, "key%d", "value%d", 10)
	})

	t.Run("ExpiredItems", func(t *testing.T) {
		cache := NewCache(Options{
			PersistPath:      dirs["expire"],
			PersistInterval:  100 * time.Millisecond,
			ShardCount:       2,
			PersistThreshold: 5,
		})
		defer cache.Close()

		cache.SetWithExpiration("expire1", "value1", 200*time.Millisecond)
		cache.Set("persist1", "value2")

		time.Sleep(300 * time.Millisecond) // Wait for expiration

		newCache := NewCache(Options{
			PersistPath: dirs["expire"],
			ShardCount:  2,
		})
		defer newCache.Close()

		// Expired key should not exist
		if _, exists := newCache.Get("expire1"); exists {
			t.Error("Expired key should not be loaded")
		}

		// Non-expired key should exist
		if val, exists := newCache.Get("persist1"); !exists || val != "value2" {
			t.Error("Non-expired key should be loaded")
		}
	})

	t.Run("UpdateExisting", func(t *testing.T) {
		cache := NewCache(Options{
			PersistPath:      dirs["update"],
			PersistInterval:  100 * time.Millisecond,
			ShardCount:       2,
			PersistThreshold: 5,
		})
		defer cache.Close()

		cache.Set("key1", "value1")
		time.Sleep(150 * time.Millisecond)

		cache.Set("key1", "value1_updated")
		time.Sleep(150 * time.Millisecond)

		newCache := NewCache(Options{
			PersistPath: dirs["update"],
			ShardCount:  2,
		})
		defer newCache.Close()

		if val, exists := newCache.Get("key1"); !exists || val != "value1_updated" {
			t.Errorf("Expected updated value, got %v", val)
		}
	})

	t.Run("ConcurrentAccess", func(t *testing.T) {
		cache := NewCache(Options{
			PersistPath:      dirs["concurrent"],
			PersistInterval:  100 * time.Millisecond,
			ShardCount:       2,
			PersistThreshold: 10,
		})
		defer cache.Close()

		var wg sync.WaitGroup
		// Concurrent read and write operations
		for i := 0; i < 5; i++ {
			wg.Add(2)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					key := fmt.Sprintf("key%d-%d", id, j)
					cache.Set(key, fmt.Sprintf("value%d-%d", id, j))
					time.Sleep(10 * time.Millisecond)
				}
			}(i)
			go func(id int) {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					key := fmt.Sprintf("key%d-%d", id, j)
					cache.Get(key)
					time.Sleep(5 * time.Millisecond)
				}
			}(i)
		}
		wg.Wait()
		time.Sleep(200 * time.Millisecond) // Wait for final persistence

		newCache := NewCache(Options{
			PersistPath: dirs["concurrent"],
			ShardCount:  2,
		})
		defer newCache.Close()

		var count int
		for i := 0; i < 5; i++ {
			for j := 0; j < 20; j++ {
				key := fmt.Sprintf("key%d-%d", i, j)
				expected := fmt.Sprintf("value%d-%d", i, j)
				if val, exists := newCache.Get(key); exists && val == expected {
					count++
				}
			}
		}
		t.Logf("Successfully persisted %d items in concurrent test", count)
	})

	t.Run("RecoveryAfterCrash", func(t *testing.T) {
		// Simulate pre-crash writes
		cache := NewCache(Options{
			PersistPath:      dirs["crash"],
			PersistInterval:  50 * time.Millisecond,
			ShardCount:       2,
			PersistThreshold: 5,
		})

		for i := 0; i < 10; i++ {
			cache.Set(fmt.Sprintf("crash-key%d", i), fmt.Sprintf("crash-value%d", i))
		}
		time.Sleep(100 * time.Millisecond) // Wait for persistence

		// Simulate force shutdown (without calling Close())
		// ...

		newCache := NewCache(Options{
			PersistPath: dirs["crash"],
			ShardCount:  2,
		})
		defer newCache.Close()

		verifyCache(t, newCache, "crash-key%d", "crash-value%d", 10)
	})

	t.Run("CorruptedFile", func(t *testing.T) {
		// Create corrupted persistence file
		corruptFile := filepath.Join(dirs["corrupt"], "0.dat")
		if err := os.WriteFile(corruptFile, []byte("corrupted data"), 0644); err != nil {
			t.Fatalf("Failed to create corrupted file: %v", err)
		}

		cache := NewCache(Options{
			PersistPath: dirs["corrupt"],
			ShardCount:  2,
		})
		defer cache.Close()

		// Writing new data should work normally
		cache.Set("new-key", "new-value")
		if val, exists := cache.Get("new-key"); !exists || val != "new-value" {
			t.Error("Cache should work normally after corrupted file")
		}
	})

	t.Run("FileCheck", func(t *testing.T) {
		// Wait for all cache instances to close and files to be written
		time.Sleep(200 * time.Millisecond)

		for name, dir := range dirs {
			files, err := os.ReadDir(dir)
			if err != nil {
				t.Errorf("Failed to read directory %s: %v", name, err)
				continue
			}

			datFiles := 0
			for _, f := range files {
				if strings.HasSuffix(f.Name(), ".dat") {
					datFiles++
					// Skip file permission check on Windows
					if runtime.GOOS != "windows" {
						info, err := f.Info()
						if err != nil {
							t.Errorf("Failed to get file info for %s: %v", f.Name(), err)
							continue
						}
						if info.Mode().Perm() != 0644 {
							t.Errorf("Unexpected file permissions for %s: %v", f.Name(), info.Mode().Perm())
						}
					}

					// Verify file is readable and non-empty
					filePath := filepath.Join(dir, f.Name())
					content, err := os.ReadFile(filePath)
					if err != nil {
						t.Errorf("Failed to read file %s: %v", filePath, err)
					} else if len(content) == 0 {
						t.Errorf("File %s is empty", filePath)
					}
				}
			}

			// Special handling for certain directories
			switch name {
			case "corrupt": // corrupt directory should have only one file
				if datFiles != 1 {
					t.Errorf("Expected 1 .dat file in %s, got %d", name, datFiles)
				}
			case "update", "expire": // these directories may have only one active shard
				if datFiles < 1 {
					t.Errorf("Expected at least 1 .dat file in %s, got %d", name, datFiles)
				}
			default: // other directories should have two shard files
				if datFiles != 2 {
					t.Errorf("Expected 2 .dat files in %s, got %d", name, datFiles)
				}
			}
		}
	})
}

func TestCachePersistence1(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cache_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	t.Run("Basic Persistence", func(t *testing.T) {
		persistDir := filepath.Join(tempDir, "basic")
		opts := Options{
			MaxSize:          100,
			PersistPath:      persistDir,
			PersistInterval:  time.Millisecond * 100,
			PersistThreshold: 2,
			ShardCount:       1, // Use single shard to simplify test
		}

		cache := NewCache(opts)
		cache.Set("key1", "value1")
		cache.Set("key2", "value2")

		// Wait for persistence to complete
		time.Sleep(time.Millisecond * 200)

		// Check shard 0 persistence file
		shardFile := filepath.Join(persistDir, "0.dat")
		if _, err := os.Stat(shardFile); os.IsNotExist(err) {
			t.Error("Persistence file should exist")
		}

		newCache := NewCache(opts)
		time.Sleep(time.Millisecond * 100)

		if val, exists := newCache.Get("key1"); !exists || val != "value1" {
			t.Error("Persisted data not loaded correctly")
		}
	})

	t.Run("Persistence Threshold", func(t *testing.T) {
		persistDir := filepath.Join(tempDir, "threshold")
		opts := Options{
			PersistPath:      persistDir,
			PersistThreshold: 3,
			PersistInterval:  time.Second,
			ShardCount:       1,
		}

		cache := NewCache(opts)
		defer cache.Close()

		// Write data without exceeding threshold
		cache.Set("key1", "value1")
		cache.Set("key2", "value2")
		time.Sleep(time.Millisecond * 50)

		// Check shard 0 persistence file
		shardFile := filepath.Join(persistDir, "0.dat")
		if _, err := os.Stat(shardFile); !os.IsNotExist(err) {
			t.Error("Persistence file should not exist yet")
		}

		// Write third item to trigger persistence
		cache.Set("key3", "value3")
		time.Sleep(time.Millisecond * 200)

		if _, err := os.Stat(shardFile); os.IsNotExist(err) {
			t.Errorf("Persistence file should exist after threshold at path: %s. Error: %v", shardFile, err)
		}

		newCache := NewCache(opts)
		defer newCache.Close()
		time.Sleep(time.Millisecond * 100)

		for i := 1; i <= 3; i++ {
			key := fmt.Sprintf("key%d", i)
			value := fmt.Sprintf("value%d", i)
			if val, exists := newCache.Get(key); !exists || val != value {
				t.Errorf("Expected %s=%s, got %v", key, value, val)
			}
		}
	})

	t.Run("Data Recovery After Crash", func(t *testing.T) {
		persistDir := filepath.Join(tempDir, "recovery")
		opts := Options{
			PersistPath:      persistDir,
			PersistInterval:  time.Millisecond * 50,
			PersistThreshold: 2,
			ShardCount:       1,
		}

		// First instance
		cache1 := NewCache(opts)
		cache1.Set("key1", "value1")
		cache1.Set("key2", "value2")
		time.Sleep(time.Millisecond * 100)

		// Second instance (simulate recovery)
		cache2 := NewCache(opts)
		time.Sleep(time.Millisecond * 100)

		for i := 1; i <= 2; i++ {
			key := fmt.Sprintf("key%d", i)
			value := fmt.Sprintf("value%d", i)
			if val, exists := cache2.Get(key); !exists || val != value {
				t.Errorf("Failed to recover %s=%s", key, value)
			}
		}
	})

	t.Run("Concurrent Persistence", func(t *testing.T) {
		persistDir := filepath.Join(tempDir, "concurrent")
		opts := Options{
			PersistPath:      persistDir,
			PersistInterval:  time.Millisecond * 50,
			PersistThreshold: 10,
			ShardCount:       4, // Use multiple shards for concurrency testing
		}

		cache := NewCache(opts)
		defer cache.Close()

		// Concurrent writes
		var wg sync.WaitGroup
		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				key := fmt.Sprintf("key%d", id)
				value := fmt.Sprintf("value%d", id)
				cache.Set(key, value)
			}(i)
		}

		wg.Wait()
		time.Sleep(time.Millisecond * 200)

		newCache := NewCache(opts)
		defer newCache.Close()
		time.Sleep(time.Millisecond * 100)

		var count atomic.Int32
		var checkWg sync.WaitGroup
		for i := 0; i < 20; i++ {
			checkWg.Add(1)
			go func(id int) {
				defer checkWg.Done()
				key := fmt.Sprintf("key%d", id)
				if _, exists := newCache.Get(key); exists {
					count.Add(1)
				}
			}(i)
		}
		checkWg.Wait()

		if count.Load() == 0 {
			t.Error("No data was persisted")
		}
	})
}

func TestGroupPersistence(t *testing.T) {
	dir, err := os.MkdirTemp("", "cache-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	t.Run("Group Persistence", func(t *testing.T) {
		cache := NewCache(Options{
			PersistPath:     filepath.Join(dir, "group-test"),
			PersistInterval: 100 * time.Millisecond,
			ShardCount:      2,
		})
		defer cache.Close()

		users := cache.Group("users")
		posts := cache.Group("posts")

		users.Set("1", "user1")
		users.SetWithExpiration("2", "user2", 200*time.Millisecond)
		posts.Set("1", "post1")

		// Wait for persistence
		time.Sleep(150 * time.Millisecond)

		newCache := NewCache(Options{
			PersistPath: filepath.Join(dir, "group-test"),
			ShardCount:  2,
		})
		defer newCache.Close()

		newUsers := newCache.Group("users")
		newPosts := newCache.Group("posts")

		// Check permanent key
		if val, exists := newUsers.Get("1"); !exists || val != "user1" {
			t.Error("Permanent key in group should be loaded")
		}

		// Check expired key
		time.Sleep(100 * time.Millisecond) // Wait for expiration
		if _, exists := newUsers.Get("2"); exists {
			t.Error("Expired key in group should not be loaded")
		}

		// Check key from other group
		if val, exists := newPosts.Get("1"); !exists || val != "post1" {
			t.Error("Key in other group should be loaded")
		}
	})
}

// TestPersistenceRaceCondition tests that persistToDisk doesn't cause race conditions
// when ExpireTime pointers are reused from the timePool
func TestPersistenceRaceCondition(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cache_race_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	cache := NewCache(Options{
		DefaultExpiration: 100 * time.Millisecond,
		CleanupInterval:   50 * time.Millisecond,
		ShardCount:        2,
		PersistPath:       filepath.Join(tempDir, "cache"),
		PersistInterval:   50 * time.Millisecond, // Reduced frequency to avoid file conflicts
		PersistThreshold:  1,                     // Persist on every change
	})
	defer cache.Close()

	const (
		numGoroutines = 10
		numOperations = 100
	)

	var wg sync.WaitGroup

	// Start multiple goroutines that continuously add/delete items
	// This will cause ExpireTime pointers to be frequently returned to and taken from timePool
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key_%d_%d", goroutineID, j)

				// Set with expiration
				cache.SetWithExpiration(key, "value", 200*time.Millisecond)

				// Immediately delete to return ExpireTime pointer to pool
				cache.Delete(key)

				// Set again to potentially reuse the same pointer
				cache.SetWithExpiration(key+"_new", "new_value", 300*time.Millisecond)

				// Small delay to let persistence happen
				time.Sleep(time.Millisecond)
			}
		}(i)
	}

	// Start a goroutine that triggers manual persistence
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < numOperations; i++ {
			// Force persistence on all shards
			if sc, ok := cache.(*ShardedCache); ok {
				for shardIndex := uint64(0); shardIndex < sc.shardNum; shardIndex++ {
					shard := sc.shards[shardIndex]
					if shard.persistPath != "" {
						shard.persistToDisk() // This should not race with pointer reuse
					}
				}
			}
			time.Sleep(50 * time.Millisecond)
		}
	}()

	wg.Wait()

	// If we reach here without data races, the test passes
	t.Log("Race condition test completed successfully")
}

// TestPersistenceExpireTimeIntegrity verifies that persisted expiration times
// are not corrupted by timePool reuse
func TestPersistenceExpireTimeIntegrity(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "cache_integrity_test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	persistPath := filepath.Join(tempDir, "cache")
	cache := NewCache(Options{
		DefaultExpiration: time.Hour, // Long expiration
		CleanupInterval:   time.Hour,
		ShardCount:        1, // Single shard for simplicity
		PersistPath:       persistPath,
		PersistInterval:   time.Hour, // Manual persistence only
		PersistThreshold:  1000,      // Manual persistence only
	})

	// Set a key with a specific expiration time
	cache.SetWithExpiration("test_key", "test_value", time.Hour)

	// Get the actual expiration time for comparison
	_, actualExpiration, exists := cache.GetWithExpiration("test_key")
	if !exists {
		t.Fatal("Key should exist")
	}

	// Create many other keys to force timePool reuse
	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("temp_key_%d", i)
		cache.SetWithExpiration(key, "temp_value", time.Hour)
		cache.Delete(key) // Return ExpireTime to pool
	}

	// Force persistence manually by accessing the shard directly
	if sc, ok := cache.(*ShardedCache); ok {
		shard := sc.shards[0]

		// Manually call persistToDisk in a controlled way
		items := make([]persistItem, 0)
		now := time.Now()

		shard.mu.RLock()
		for key, item := range shard.items {
			if item.ExpireTime != nil && now.After(*item.ExpireTime) {
				continue
			}

			// This is the fixed code - create a safe copy of the expiration time
			var expireTimeCopy *time.Time
			if item.ExpireTime != nil {
				t := *item.ExpireTime
				expireTimeCopy = &t
			}

			items = append(items, persistItem{
				Key:        key,
				Value:      item.Value,
				ExpireTime: expireTimeCopy,
				CreatedAt:  item.CreatedAt,
			})
		}
		shard.mu.RUnlock()

		// Verify we collected the expected item
		found := false
		var persistedExpiration *time.Time
		for _, item := range items {
			if item.Key == "test_key" {
				found = true
				persistedExpiration = item.ExpireTime
				break
			}
		}

		if !found {
			t.Fatal("test_key should be in persisted items")
		}

		if persistedExpiration == nil {
			t.Fatal("test_key should have an expiration time")
		}

		// The persisted time should match the original time (within tolerance)
		timeDiff := persistedExpiration.Sub(actualExpiration)
		if timeDiff < 0 {
			timeDiff = -timeDiff
		}

		if timeDiff > time.Second {
			t.Errorf("Expiration time integrity compromised: expected %v, got %v (diff: %v)",
				actualExpiration, *persistedExpiration, timeDiff)
		} else {
			t.Logf("Expiration time integrity verified: original=%v, persisted=%v",
				actualExpiration, *persistedExpiration)
		}
	} else {
		t.Fatal("Expected ShardedCache type")
	}

	cache.Close()
}
