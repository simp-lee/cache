package shardedcache

import (
	"encoding/gob"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

var (
	itemPool = sync.Pool{
		New: func() interface{} {
			return &cacheItem{}
		},
	}

	timePool = sync.Pool{
		New: func() interface{} {
			return new(time.Time)
		},
	}
)

// cacheShard represents a single shard of the cache implementation
type cacheShard struct {
	items             map[string]*cacheItem
	maxSize           int
	defaultExpiration time.Duration
	onEvicted         func(string, interface{})
	hits              uint64
	misses            uint64
	mu                sync.RWMutex
	persistPath       string
	modifyCount       int32
	persistThreshold  int
	persistChan       chan struct{}
	stopChan          chan struct{}
	wg                sync.WaitGroup
}

// cacheItem represents a single item in the cache
type cacheItem struct {
	Value      interface{}
	ExpireTime *time.Time
	CreatedAt  time.Time
}

func newCacheShard(opts Options, persistPath string) *cacheShard {
	cs := &cacheShard{
		items:             make(map[string]*cacheItem),
		maxSize:           opts.MaxSize,
		defaultExpiration: opts.DefaultExpiration,
		persistPath:       persistPath,
		persistThreshold:  opts.PersistThreshold,
		persistChan:       make(chan struct{}, 1),
		stopChan:          make(chan struct{}),
	}

	// Load persisted data
	if persistPath != "" {
		if err := cs.loadFromDisk(); err != nil {
			log.Println("Failed to load cache from disk:", err)
		}
	}

	if opts.CleanupInterval > 0 {
		cs.wg.Add(1)
		go cs.startCleaner(opts.CleanupInterval)
	}

	if persistPath != "" && opts.PersistInterval > 0 {
		cs.wg.Add(1)
		go cs.startPersister(opts.PersistInterval)
	}

	return cs
}

func (cs *cacheShard) set(key string, value interface{}) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.setWithExpirationUnlocked(key, value, cs.defaultExpiration)
}

func (cs *cacheShard) setWithExpiration(key string, value interface{}, expiration time.Duration) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.setWithExpirationUnlocked(key, value, expiration)
}

func (cs *cacheShard) setWithExpirationUnlocked(key string, value interface{}, expiration time.Duration) {
	item := itemPool.Get().(*cacheItem)

	if expiration > 0 {
		t := timePool.Get().(*time.Time)
		*t = time.Now().Add(expiration)
		item.ExpireTime = t
	} else {
		item.ExpireTime = nil
	}

	// Set other fields
	item.Value = value
	item.CreatedAt = time.Now()

	// If the maximum capacity is reached, delete the oldest item
	if cs.maxSize > 0 && len(cs.items) >= cs.maxSize && cs.items[key] == nil {
		cs.evictOne()
	}

	// If the item already exists, recycle the old object
	if oldItem := cs.items[key]; oldItem != nil {
		if oldItem.ExpireTime != nil {
			timePool.Put(oldItem.ExpireTime)
		}
		itemPool.Put(oldItem)
	}

	cs.items[key] = item

	// Modify count and possibly trigger persistence
	if cs.persistPath != "" {
		count := atomic.AddInt32(&cs.modifyCount, 1)
		if count >= int32(cs.persistThreshold) {
			select {
			case cs.persistChan <- struct{}{}: // Trigger persistence
			default: // Persistence queue is full, discard
			}
		}
	}
}

func (cs *cacheShard) get(key string) (interface{}, bool) {
	cs.mu.RLock()
	item, found := cs.items[key]
	if !found {
		cs.mu.RUnlock()
		atomic.AddUint64(&cs.misses, 1)
		return nil, false
	}

	// Check if the item has expired
	if item.ExpireTime != nil && time.Now().After(*item.ExpireTime) {
		cs.mu.RUnlock()
		atomic.AddUint64(&cs.misses, 1)
		cs.delete(key)
		return nil, false
	}

	cs.mu.RUnlock()
	atomic.AddUint64(&cs.hits, 1)
	return item.Value, true
}

func (cs *cacheShard) getWithExpiration(key string) (interface{}, *time.Time, bool) {
	cs.mu.RLock()
	item, found := cs.items[key]
	if !found {
		cs.mu.RUnlock()
		atomic.AddUint64(&cs.misses, 1)
		return nil, nil, false
	}

	// Check if the item has expired
	if item.ExpireTime != nil && time.Now().After(*item.ExpireTime) {
		cs.mu.RUnlock()
		atomic.AddUint64(&cs.misses, 1)
		cs.delete(key)
		return nil, nil, false
	}

	cs.mu.RUnlock()
	atomic.AddUint64(&cs.hits, 1)
	return item.Value, item.ExpireTime, true
}

func (cs *cacheShard) delete(key string) error {
	cs.mu.Lock()
	if item := cs.items[key]; item != nil {
		if cs.onEvicted != nil {
			cs.onEvicted(key, item.Value)
		}
		if item.ExpireTime != nil {
			timePool.Put(item.ExpireTime)
		}
		itemPool.Put(item)
		delete(cs.items, key)
	}
	cs.mu.Unlock()
	return nil
}

// deletePrefix deletes all keys with the given prefix in this shard and returns the number of deleted keys.
func (cs *cacheShard) deletePrefix(prefix string) int {
	if prefix == "" {
		// Avoid accidental mass deletion; use Clear() explicitly for full wipe
		return 0
	}
	// Collect matching keys under read lock
	cs.mu.RLock()
	keys := make([]string, 0)
	for k := range cs.items {
		if strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	cs.mu.RUnlock()

	// Delete outside of read lock to minimize blocking
	for _, k := range keys {
		cs.delete(k)
	}

	// Trigger persistence threshold if configured
	if n := len(keys); n > 0 && cs.persistPath != "" {
		count := atomic.AddInt32(&cs.modifyCount, int32(n))
		if count >= int32(cs.persistThreshold) {
			select {
			case cs.persistChan <- struct{}{}:
			default:
			}
		}
	}

	return len(keys)
}

// deleteKeys deletes specified keys that belong to this shard and returns the number of actually deleted keys.
func (cs *cacheShard) deleteKeys(keys []string) int {
	if len(keys) == 0 {
		return 0
	}

	// Deduplicate keys first
	keySet := make(map[string]struct{})
	for _, k := range keys {
		keySet[k] = struct{}{}
	}

	// Check existence under read lock to count accurately
	existing := make([]string, 0, len(keySet))
	cs.mu.RLock()
	for k := range keySet {
		if cs.items[k] != nil {
			existing = append(existing, k)
		}
	}
	cs.mu.RUnlock()

	for _, k := range existing {
		cs.delete(k)
	}

	if n := len(existing); n > 0 && cs.persistPath != "" {
		count := atomic.AddInt32(&cs.modifyCount, int32(n))
		if count >= int32(cs.persistThreshold) {
			select {
			case cs.persistChan <- struct{}{}:
			default:
			}
		}
	}

	return len(existing)
}

func (cs *cacheShard) evictOne() {
	var oldestKey string
	var oldestTime time.Time
	first := true

	for k, v := range cs.items {
		if first {
			oldestKey = k
			oldestTime = v.CreatedAt
			first = false
			continue
		}
		if v.CreatedAt.Before(oldestTime) {
			oldestKey = k
			oldestTime = v.CreatedAt
		}
	}

	if oldestKey != "" {
		if item := cs.items[oldestKey]; item != nil {
			if cs.onEvicted != nil {
				cs.onEvicted(oldestKey, item.Value)
			}
			if item.ExpireTime != nil {
				timePool.Put(item.ExpireTime)
			}
			itemPool.Put(item)
			delete(cs.items, oldestKey)
		}
	}
}

func (cs *cacheShard) startCleaner(interval time.Duration) {
	defer cs.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			cs.deleteExpired()
		case <-cs.stopChan:
			return
		}
	}
}

func (cs *cacheShard) startPersister(interval time.Duration) {
	defer cs.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C: // Scheduled persistence
			if atomic.LoadInt32(&cs.modifyCount) > 0 {
				if err := cs.persistToDisk(); err != nil {
					log.Printf("Failed to persist cache to disk %s: %v", cs.persistPath, err)
				}
				atomic.StoreInt32(&cs.modifyCount, 0)
			}
		case <-cs.persistChan: // Persistence triggered by threshold
			if err := cs.persistToDisk(); err != nil {
				log.Printf("Failed to persist cache to disk %s: %v", cs.persistPath, err)
			}
			atomic.StoreInt32(&cs.modifyCount, 0)
		case <-cs.stopChan: // Perform last persistence before shutdown
			if atomic.LoadInt32(&cs.modifyCount) > 0 {
				if err := cs.persistToDisk(); err != nil {
					log.Printf("Failed to persist cache to disk on shutdown %s: %v", cs.persistPath, err)
				}
			}
			return
		}
	}
}

func (cs *cacheShard) deleteExpired() {
	now := time.Now()
	cs.mu.Lock()
	for k, v := range cs.items {
		if v.ExpireTime != nil && now.After(*v.ExpireTime) {
			if cs.onEvicted != nil {
				cs.onEvicted(k, v.Value)
			}
			if v.ExpireTime != nil {
				timePool.Put(v.ExpireTime)
			}
			itemPool.Put(v)
			delete(cs.items, k)
		}
	}
	cs.mu.Unlock()
}

func (cs *cacheShard) close() {
	close(cs.stopChan)
	cs.wg.Wait()
}

// persistItem represents the structure used for persisting cache items to disk
type persistItem struct {
	Key        string
	Value      interface{}
	ExpireTime *time.Time
	CreatedAt  time.Time
}

func (cs *cacheShard) persistToDisk() error {
	if cs.persistPath == "" {
		return nil
	}

	items := make([]persistItem, 0, len(cs.items))

	// Acquire a read lock to get a snapshot of the current data
	cs.mu.RLock()
	for key, item := range cs.items {
		// Skip expired items
		if item.ExpireTime != nil && time.Now().After(*item.ExpireTime) {
			continue
		}
		items = append(items, persistItem{
			Key:        key,
			Value:      item.Value,
			ExpireTime: item.ExpireTime,
			CreatedAt:  item.CreatedAt,
		})
	}
	cs.mu.RUnlock()

	tempFile := cs.persistPath + ".tmp"
	file, err := os.Create(tempFile)
	if err != nil {
		return fmt.Errorf("failed to create temp file: %v", err)
	}

	encoder := gob.NewEncoder(file)
	if err := encoder.Encode(items); err != nil {
		file.Close()
		os.Remove(tempFile)
		return fmt.Errorf("failed to encode cache items: %v", err)
	}

	if err := file.Close(); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to close temp file: %v", err)
	}

	if err := os.Rename(tempFile, cs.persistPath); err != nil {
		os.Remove(tempFile)
		return fmt.Errorf("failed to rename temp file: %v", err)
	}

	return nil
}

func (cs *cacheShard) loadFromDisk() error {
	if cs.persistPath == "" {
		return nil
	}

	file, err := os.Open(cs.persistPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to open persistence file: %v", err)
	}
	defer file.Close()

	var items []persistItem
	decoder := gob.NewDecoder(file)
	if err := decoder.Decode(&items); err != nil {
		return fmt.Errorf("failed to decode cache items: %v", err)
	}

	now := time.Now()
	cs.mu.Lock()

	for _, item := range items {
		if item.ExpireTime != nil && now.After(*item.ExpireTime) {
			continue
		}
		cs.items[item.Key] = &cacheItem{
			Value:      item.Value,
			ExpireTime: item.ExpireTime,
			CreatedAt:  item.CreatedAt,
		}
	}
	cs.mu.Unlock()

	return nil
}
