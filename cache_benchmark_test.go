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

	"context"

	"github.com/allegro/bigcache/v3"
	gocache "github.com/patrickmn/go-cache"
)

func BenchmarkPersistToDisk(b *testing.B) {
	tempDir, _ := os.MkdirTemp("", "cache_bench")
	defer os.RemoveAll(tempDir)

	dataSizes := []int{1000, 10000, 100000, 1000000}

	for _, size := range dataSizes {
		b.Run(fmt.Sprintf("Size-%d", size), func(b *testing.B) {
			// 准备数据
			persistPath := filepath.Join(tempDir, fmt.Sprintf("cache-%d", size))
			c := NewCache(Options{
				PersistPath: persistPath,
			})

			// 类型断言获取ShardedCache
			shardedCache, ok := c.(*ShardedCache)
			if !ok {
				b.Fatalf("Expected ShardedCache, got %T", c)
			}

			// 填充数据
			for i := 0; i < size; i++ {
				key := fmt.Sprintf("key-%d", i)
				value := fmt.Sprintf("value-%d", i)
				c.Set(key, value)
			}

			b.ResetTimer()
			// 测试持久化性能
			for i := 0; i < b.N; i++ {
				if shard := shardedCache.getShard("trigger-key"); shard != nil {
					shard.persistToDisk()
				}
			}

			c.Close() // 最后关闭缓存
		})
	}
}

func BenchmarkShardedCache(b *testing.B) {
	cache := NewCache(Options{
		ShardCount: 32,
		MaxSize:    1000000,
	})

	// 预填充一些数据
	for i := 0; i < 100000; i++ {
		cache.Set(fmt.Sprintf("key-%d", i), i)
	}

	b.Run("Set", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			var i int64
			for pb.Next() {
				key := fmt.Sprintf("key-%d", atomic.AddInt64(&i, 1))
				cache.Set(key, i)
			}
		})
	})

	b.Run("Get/Hit", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			var i int64
			for pb.Next() {
				key := fmt.Sprintf("key-%d", atomic.AddInt64(&i, 1)%100000)
				cache.Get(key)
			}
		})
	})

	b.Run("Get/Miss", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			var i int64
			for pb.Next() {
				key := fmt.Sprintf("missing-key-%d", atomic.AddInt64(&i, 1))
				cache.Get(key)
			}
		})
	})

	b.Run("Mixed/80-20", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			var i int64
			for pb.Next() {
				n := atomic.AddInt64(&i, 1)
				key := fmt.Sprintf("key-%d", n%100000)
				if n%5 == 0 { // 20% 写入
					cache.Set(key, n)
				} else { // 80% 读取
					cache.Get(key)
				}
			}
		})
	})

	b.Run("SetWithExpiration", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			var i int64
			for pb.Next() {
				n := atomic.AddInt64(&i, 1)
				key := fmt.Sprintf("exp-key-%d", n)
				expiration := time.Now().Add(time.Minute)
				cache.SetWithExpiration(key, n, time.Until(expiration))
			}
		})
	})

	b.Run("Delete", func(b *testing.B) {
		// 先预填充一些数据用于删��
		for i := 0; i < 100000; i++ {
			cache.Set(fmt.Sprintf("del-key-%d", i), i)
		}

		b.RunParallel(func(pb *testing.PB) {
			var i int64
			for pb.Next() {
				key := fmt.Sprintf("del-key-%d", atomic.AddInt64(&i, 1)%100000)
				cache.Delete(key)
			}
		})
	})
}

func BenchmarkCacheComparison(b *testing.B) {
	// 创建不同类型的缓存实现
	shardedCache := NewCache(Options{
		ShardCount:        32,
		MaxSize:           1000000,
		DefaultExpiration: 5 * time.Minute,
		CleanupInterval:   10 * time.Minute,
	})

	goCache := gocache.New(5*time.Minute, 10*time.Minute)

	bigCacheConfig := bigcache.DefaultConfig(5 * time.Minute)
	bigCacheConfig.Verbose = false // 关闭日志
	bigCache, err := bigcache.New(context.Background(), bigCacheConfig)
	if err != nil {
		b.Fatal(err)
	}

	simpleMap := make(map[string]string)
	var mapMutex sync.RWMutex

	// 预填充数据
	for i := 0; i < 1000000; i++ {
		key := fmt.Sprintf("key%d", i)
		value := fmt.Sprintf("value%d", i)
		shardedCache.Set(key, value)
		goCache.Set(key, value, gocache.DefaultExpiration)
		bigCache.Set(key, []byte(value))
		mapMutex.Lock()
		simpleMap[key] = value
		mapMutex.Unlock()
	}

	benchmarks := []struct {
		name string
		fn   func(b *testing.B)
	}{
		{"ShardedCache/Get", func(b *testing.B) {
			var hits int64
			b.RunParallel(func(pb *testing.PB) {
				localHits := 0
				for pb.Next() {
					key := fmt.Sprintf("key%d", rand.Intn(1000000))
					if val, ok := shardedCache.Get(key); ok && val.(string) == simpleMap[key] {
						localHits++
					}
				}
				atomic.AddInt64(&hits, int64(localHits))
			})
			b.ReportMetric(float64(hits)/float64(b.N)*100, "hit%")
		}},
		{"GoCache/Get", func(b *testing.B) {
			var hits int64
			b.RunParallel(func(pb *testing.PB) {
				localHits := 0
				for pb.Next() {
					key := fmt.Sprintf("key%d", rand.Intn(1000000))
					if val, found := goCache.Get(key); found && val.(string) == simpleMap[key] {
						localHits++
					}
				}
				atomic.AddInt64(&hits, int64(localHits))
			})
			b.ReportMetric(float64(hits)/float64(b.N)*100, "hit%")
		}},
		{"SyncMap/Get", func(b *testing.B) {
			var hits int64
			b.RunParallel(func(pb *testing.PB) {
				localHits := 0
				for pb.Next() {
					key := fmt.Sprintf("key%d", rand.Intn(1000000))
					mapMutex.RLock()
					val, ok := simpleMap[key]
					mapMutex.RUnlock()
					if ok && val == simpleMap[key] {
						localHits++
					}
				}
				atomic.AddInt64(&hits, int64(localHits))
			})
			b.ReportMetric(float64(hits)/float64(b.N)*100, "hit%")
		}},
		{"BigCache/Get", func(b *testing.B) {
			var hits int64
			b.RunParallel(func(pb *testing.PB) {
				localHits := 0
				for pb.Next() {
					key := fmt.Sprintf("key%d", rand.Intn(1000000))
					if val, err := bigCache.Get(key); err == nil {
						if string(val) == simpleMap[key] {
							localHits++
						}
					}
				}
				atomic.AddInt64(&hits, int64(localHits))
			})
			b.ReportMetric(float64(hits)/float64(b.N)*100, "hit%")
		}},
		{"ShardedCache/Set", func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				var i int64
				for pb.Next() {
					n := atomic.AddInt64(&i, 1)
					key := fmt.Sprintf("new-key%d", n)
					shardedCache.Set(key, fmt.Sprintf("value%d", n))
				}
			})
		}},
		{"GoCache/Set", func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				var i int64
				for pb.Next() {
					n := atomic.AddInt64(&i, 1)
					key := fmt.Sprintf("new-key%d", n)
					goCache.Set(key, fmt.Sprintf("value%d", n), gocache.DefaultExpiration)
				}
			})
		}},
		{"SyncMap/Set", func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				var i int64
				for pb.Next() {
					n := atomic.AddInt64(&i, 1)
					key := fmt.Sprintf("new-key%d", n)
					mapMutex.Lock()
					simpleMap[key] = fmt.Sprintf("value%d", n)
					mapMutex.Unlock()
				}
			})
		}},
		{"BigCache/Set", func(b *testing.B) {
			b.RunParallel(func(pb *testing.PB) {
				var i int64
				for pb.Next() {
					n := atomic.AddInt64(&i, 1)
					key := fmt.Sprintf("new-key%d", n)
					bigCache.Set(key, []byte(fmt.Sprintf("value%d", n)))
				}
			})
		}},
		{"ShardedCache/Mixed", func(b *testing.B) {
			var hits int64
			b.RunParallel(func(pb *testing.PB) {
				localHits := 0
				var i int64
				for pb.Next() {
					n := atomic.AddInt64(&i, 1)
					if n%5 == 0 { // 20% writes
						key := fmt.Sprintf("new-key%d", n)
						shardedCache.Set(key, fmt.Sprintf("value%d", n))
					} else { // 80% reads
						key := fmt.Sprintf("key%d", rand.Intn(1000000))
						if val, ok := shardedCache.Get(key); ok && val.(string) == simpleMap[key] {
							localHits++
						}
					}
				}
				atomic.AddInt64(&hits, int64(localHits))
			})
			b.ReportMetric(float64(hits)/float64(b.N*4/5)*100, "hit%")
		}},
		{"GoCache/Mixed", func(b *testing.B) {
			var hits int64
			b.RunParallel(func(pb *testing.PB) {
				localHits := 0
				var i int64
				for pb.Next() {
					n := atomic.AddInt64(&i, 1)
					if n%5 == 0 { // 20% writes
						key := fmt.Sprintf("new-key%d", n)
						goCache.Set(key, fmt.Sprintf("value%d", n), gocache.DefaultExpiration)
					} else { // 80% reads
						key := fmt.Sprintf("key%d", rand.Intn(1000000))
						if val, found := goCache.Get(key); found && val.(string) == simpleMap[key] {
							localHits++
						}
					}
				}
				atomic.AddInt64(&hits, int64(localHits))
			})
			b.ReportMetric(float64(hits)/float64(b.N*4/5)*100, "hit%")
		}},
		{"SyncMap/Mixed", func(b *testing.B) {
			var hits int64
			b.RunParallel(func(pb *testing.PB) {
				localHits := 0
				var i int64
				for pb.Next() {
					n := atomic.AddInt64(&i, 1)
					if n%5 == 0 { // 20% writes
						key := fmt.Sprintf("new-key%d", n)
						mapMutex.Lock()
						simpleMap[key] = fmt.Sprintf("value%d", n)
						mapMutex.Unlock()
					} else { // 80% reads
						idx := rand.Intn(1000000)
						key := fmt.Sprintf("key%d", idx)
						mapMutex.RLock()
						val, ok := simpleMap[key]
						expectedVal := fmt.Sprintf("value%d", idx)
						mapMutex.RUnlock()

						if ok && val == expectedVal {
							localHits++
						}
					}
				}
				atomic.AddInt64(&hits, int64(localHits))
			})
			b.ReportMetric(float64(hits)/float64(b.N*4/5)*100, "hit%")
		}},
		{"BigCache/Mixed", func(b *testing.B) {
			var hits int64
			b.RunParallel(func(pb *testing.PB) {
				localHits := 0
				var i int64
				for pb.Next() {
					n := atomic.AddInt64(&i, 1)
					if n%5 == 0 { // 20% writes
						key := fmt.Sprintf("new-key%d", n)
						bigCache.Set(key, []byte(fmt.Sprintf("value%d", n)))
					} else { // 80% reads
						idx := rand.Intn(1000000)
						key := fmt.Sprintf("key%d", idx)
						if val, err := bigCache.Get(key); err == nil {
							if string(val) == simpleMap[key] {
								localHits++
							}
						}
					}
				}
				atomic.AddInt64(&hits, int64(localHits))
			})
			b.ReportMetric(float64(hits)/float64(b.N*4/5)*100, "hit%")
		}},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			bm.fn(b)
		})
	}

	// 报告内存使用情况
	b.Run("MemoryUsage", func(b *testing.B) {
		measureMemory := func(f func()) float64 {
			runtime.GC()
			var m1, m2 runtime.MemStats
			runtime.ReadMemStats(&m1)

			f() // 执行测试函数

			runtime.GC()
			runtime.ReadMemStats(&m2)
			if m2.HeapAlloc > m1.HeapAlloc {
				return float64(m2.HeapAlloc-m1.HeapAlloc) / (1024 * 1024)
			}
			return float64(m2.HeapAlloc) / (1024 * 1024)
		}

		// 清空所有现有数据并等待GC完成
		shardedCache = nil
		goCache = nil
		simpleMap = nil
		runtime.GC()
		time.Sleep(time.Millisecond * 100) // 给GC一些时间完成

		// 测试 ShardedCache 内存
		memUsed := measureMemory(func() {
			shardedCache = NewCache(Options{
				ShardCount:        32,
				MaxSize:           1000000,
				DefaultExpiration: 5 * time.Minute,
			})
			for i := 0; i < 1000000; i++ {
				shardedCache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
			}
		})
		b.Logf("ShardedCache - Items: %d, Memory: %.2f MB",
			shardedCache.Count(), memUsed)

		// 清理并等待
		shardedCache = nil
		runtime.GC()
		time.Sleep(time.Millisecond * 100)

		// 测试 GoCache 内存
		memUsed = measureMemory(func() {
			goCache = gocache.New(5*time.Minute, 10*time.Minute)
			for i := 0; i < 1000000; i++ {
				goCache.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i), gocache.DefaultExpiration)
			}
		})
		b.Logf("GoCache - Items: %d, Memory: %.2f MB",
			goCache.ItemCount(), memUsed)

		// 清理并等待
		goCache = nil
		runtime.GC()
		time.Sleep(time.Millisecond * 100)

		// 测试 SyncMap 内存
		memUsed = measureMemory(func() {
			simpleMap = make(map[string]string, 1000000) // 预分配容量
			for i := 0; i < 1000000; i++ {
				simpleMap[fmt.Sprintf("key%d", i)] = fmt.Sprintf("value%d", i)
			}
		})
		b.Logf("SyncMap - Items: %d, Memory: %.2f MB",
			len(simpleMap), memUsed)

		// 测试 BigCache 内存
		bigCache = nil
		runtime.GC()
		time.Sleep(time.Millisecond * 100)

		memUsed = measureMemory(func() {
			bigCacheConfig := bigcache.DefaultConfig(5 * time.Minute)
			bigCacheConfig.Verbose = false // 关闭日志
			bigCache, err = bigcache.New(context.Background(), bigCacheConfig)
			if err != nil {
				b.Fatal(err)
			}
			for i := 0; i < 1000000; i++ {
				bigCache.Set(fmt.Sprintf("key%d", i), []byte(fmt.Sprintf("value%d", i)))
			}
		})
		b.Logf("BigCache - Items: %d, Memory: %.2f MB",
			bigCache.Len(), memUsed)
	})
}

func BenchmarkConcurrentPersist(b *testing.B) {
	tempDir, _ := os.MkdirTemp("", "cache_bench")
	defer os.RemoveAll(tempDir)

	dataSizes := []int{1000, 10000, 100000, 1000000}

	for _, size := range dataSizes {
		b.Run(fmt.Sprintf("Size-%d", size), func(b *testing.B) {
			// 准备数据
			persistPath := filepath.Join(tempDir, fmt.Sprintf("cache-%d", size))
			c := NewCache(Options{
				PersistPath: persistPath,
			})

			// 类型断言获取ShardedCache
			shardedCache, ok := c.(*ShardedCache)
			if !ok {
				b.Fatalf("Expected ShardedCache, got %T", c)
			}

			// 填充数据
			for i := 0; i < size; i++ {
				key := fmt.Sprintf("key-%d", i)
				value := fmt.Sprintf("value-%d", i)
				c.Set(key, value)
			}

			b.ResetTimer()
			// 测试并发持久化性能
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if shard := shardedCache.getShard(fmt.Sprintf("key-%d", rand.Int())); shard != nil {
						shard.persistToDisk()
					}
				}
			})

			c.Close() // 最后关闭缓存
		})
	}
}

func BenchmarkMixedLoadWithPersist(b *testing.B) {
	tempDir, _ := os.MkdirTemp("", "cache_bench")
	defer os.RemoveAll(tempDir)

	dataSizes := []int{1000, 10000, 100000, 1000000}

	for _, size := range dataSizes {
		b.Run(fmt.Sprintf("Size-%d", size), func(b *testing.B) {
			// 准备数据
			persistPath := filepath.Join(tempDir, fmt.Sprintf("cache-%d", size))
			c := NewCache(Options{
				PersistPath: persistPath,
			})

			// 类型断言获取ShardedCache
			shardedCache, ok := c.(*ShardedCache)
			if !ok {
				b.Fatalf("Expected ShardedCache, got %T", c)
			}

			// 填充数据
			for i := 0; i < size; i++ {
				key := fmt.Sprintf("key-%d", i)
				value := fmt.Sprintf("value-%d", i)
				c.Set(key, value)
			}

			b.ResetTimer()
			// 测试混合负载性能
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if rand.Float32() < 0.1 { // 10% 持久化操作
						if shard := shardedCache.getShard("trigger-key"); shard != nil {
							shard.persistToDisk()
						}
					} else { // 90% 正常读写操作
						// ... 读写操作 ...
					}
				}
			})

			c.Close() // 最后关闭缓存
		})
	}
}
