package cache

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
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "cache_test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 为不同的测试场景创建子目录
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

	// 创建所有需要的目录
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("Failed to create directory %s: %v", dir, err)
		}
	}

	// 添加验证函数
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

		// 写入数据
		for i := 0; i < 10; i++ {
			cache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		// 等待自动持久化
		time.Sleep(300 * time.Millisecond)

		// 创建新缓存实例验证数据
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

		// 创建一个辅助函数来检查key会被分配到哪个分片
		getShardIndex := func(key string) int {
			hash := xxhash.Sum64String(key)
			return int(hash & 1) // 因为 shardCount = 2，所以用 & 1
		}

		// 为每个分片准备精确的数据量
		shardData := make(map[int][]struct{ key, value string })
		keyIndex := 0

		// 确保每个分片正好有 threshold 个数据
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

		// 先写入 threshold-1 个数据
		for shard := 0; shard < shardCount; shard++ {
			for i := 0; i < threshold-1; i++ {
				data := shardData[shard][i]
				cache.Set(data.key, data.value)
				time.Sleep(100 * time.Millisecond)
			}
		}

		// 验证文件不存在
		time.Sleep(100 * time.Millisecond)
		files, _ := os.ReadDir(dirs["threshold"])
		if len(files) > 0 {
			t.Error("No files should exist before threshold")
		}

		// 写入最后一个数据，应该正好触发持久化
		for shard := 0; shard < shardCount; shard++ {
			lastData := shardData[shard][threshold-1]
			t.Logf("Writing final data to shard %d: key=%s, value=%s", shard, lastData.key, lastData.value)
			cache.Set(lastData.key, lastData.value)
			// 等待持久化完成
			time.Sleep(100 * time.Millisecond)
		}

		// 等待持久化完成
		time.Sleep(100 * time.Millisecond)

		// 验证文件
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

		// 验证数据
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

		// 写入数据
		for i := 0; i < 10; i++ {
			cache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		cache.Close()

		// 验证数据
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

		// 写入包含过期时间的数据
		cache.SetWithExpiration("expire1", "value1", 200*time.Millisecond)
		cache.Set("persist1", "value2")

		time.Sleep(300 * time.Millisecond) // 等待数据过期

		// 验证新实例
		newCache := NewCache(Options{
			PersistPath: dirs["expire"],
			ShardCount:  2,
		})
		defer newCache.Close()

		// 过期的键该不存在
		if _, exists := newCache.Get("expire1"); exists {
			t.Error("Expired key should not be loaded")
		}

		// 未过期的键应该存在
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

		// 写入初始数据
		cache.Set("key1", "value1")
		time.Sleep(150 * time.Millisecond)

		// 更新数据
		cache.Set("key1", "value1_updated")
		time.Sleep(150 * time.Millisecond)

		// 验证更新后的数据
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
		// 并发写入和读取
		for i := 0; i < 5; i++ {
			wg.Add(2)
			go func(id int) {
				defer wg.Done()
				// 写入操作
				for j := 0; j < 20; j++ {
					key := fmt.Sprintf("key%d-%d", id, j)
					cache.Set(key, fmt.Sprintf("value%d-%d", id, j))
					time.Sleep(10 * time.Millisecond)
				}
			}(i)
			go func(id int) {
				defer wg.Done()
				// 读取操作
				for j := 0; j < 20; j++ {
					key := fmt.Sprintf("key%d-%d", id, j)
					cache.Get(key)
					time.Sleep(5 * time.Millisecond)
				}
			}(i)
		}
		wg.Wait()
		time.Sleep(200 * time.Millisecond) // 等待最后的持久化完成

		// 验证数据持久化
		newCache := NewCache(Options{
			PersistPath: dirs["concurrent"],
			ShardCount:  2,
		})
		defer newCache.Close()

		// 验证写入的数据
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
		// 模拟崩溃前的写入
		cache := NewCache(Options{
			PersistPath:      dirs["crash"],
			PersistInterval:  50 * time.Millisecond,
			ShardCount:       2,
			PersistThreshold: 5,
		})

		for i := 0; i < 10; i++ {
			cache.Set(fmt.Sprintf("crash-key%d", i), fmt.Sprintf("crash-value%d", i))
		}
		time.Sleep(100 * time.Millisecond) // 等待持久化

		// 模拟强制关闭（不调用 Close()）
		// ...

		// 验证恢复
		newCache := NewCache(Options{
			PersistPath: dirs["crash"],
			ShardCount:  2,
		})
		defer newCache.Close()

		verifyCache(t, newCache, "crash-key%d", "crash-value%d", 10)
	})

	t.Run("CorruptedFile", func(t *testing.T) {
		// 创建损坏的持久化文件
		corruptFile := filepath.Join(dirs["corrupt"], "0.dat")
		if err := os.WriteFile(corruptFile, []byte("corrupted data"), 0644); err != nil {
			t.Fatalf("Failed to create corrupted file: %v", err)
		}

		// 验证缓存能够正常初始化
		cache := NewCache(Options{
			PersistPath: dirs["corrupt"],
			ShardCount:  2,
		})
		defer cache.Close()

		// 写入新数据该正常工作
		cache.Set("new-key", "new-value")
		if val, exists := cache.Get("new-key"); !exists || val != "new-value" {
			t.Error("Cache should work normally after corrupted file")
		}
	})

	t.Run("FileCheck", func(t *testing.T) {
		// 先关闭所有缓存实例确保文件写入
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
					// Windows 系统不需要检查文件权限
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

					// 验证文件可读性和非空
					filePath := filepath.Join(dir, f.Name())
					content, err := os.ReadFile(filePath)
					if err != nil {
						t.Errorf("Failed to read file %s: %v", filePath, err)
					} else if len(content) == 0 {
						t.Errorf("File %s is empty", filePath)
					}
				}
			}

			// 特殊处理某些目录
			switch name {
			case "corrupt": // corrupt 目录应该只有一个文件
				if datFiles != 1 {
					t.Errorf("Expected 1 .dat file in %s, got %d", name, datFiles)
				}
			case "update", "expire": // 这些目录可能只有一个活跃的分片
				if datFiles < 1 {
					t.Errorf("Expected at least 1 .dat file in %s, got %d", name, datFiles)
				}
			default: // 其他目录应该有两个分片文件
				if datFiles != 2 {
					t.Errorf("Expected 2 .dat files in %s, got %d", name, datFiles)
				}
			}
		}
	})
}

func TestCachePersistence1(t *testing.T) {
	// 创建临时目录用于测试
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
			ShardCount:       1, // 使用单分片简化测试
		}

		cache := NewCache(opts)
		cache.Set("key1", "value1")
		cache.Set("key2", "value2")

		// 等待持久化完成
		time.Sleep(time.Millisecond * 200)

		// 检查分片0的持久化文件
		shardFile := filepath.Join(persistDir, "0.dat")
		if _, err := os.Stat(shardFile); os.IsNotExist(err) {
			t.Error("Persistence file should exist")
		}

		// 创建新的缓存实例
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

		// 写入数据但不超过阈值
		cache.Set("key1", "value1")
		cache.Set("key2", "value2")
		time.Sleep(time.Millisecond * 50)

		// 检查分片0的持久化文件
		shardFile := filepath.Join(persistDir, "0.dat")
		if _, err := os.Stat(shardFile); !os.IsNotExist(err) {
			t.Error("Persistence file should not exist yet")
		}

		// 写入第三条数据触发持久化
		cache.Set("key3", "value3")
		time.Sleep(time.Millisecond * 200)

		if _, err := os.Stat(shardFile); os.IsNotExist(err) {
			t.Errorf("Persistence file should exist after threshold at path: %s. Error: %v", shardFile, err)
		}

		// 验证数据
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

		// 第一个实例
		cache1 := NewCache(opts)
		cache1.Set("key1", "value1")
		cache1.Set("key2", "value2")
		time.Sleep(time.Millisecond * 100)

		// 第二个实例（模拟恢复）
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
			ShardCount:       4, // 使用多个分片测试并发
		}

		cache := NewCache(opts)
		defer cache.Close()

		// 并发写入
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

		// 验证持久化
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
