package ttlcache

import (
	"sync"
	"time"
)

type T struct {
	sync.RWMutex
	data    string
	expires *time.Time
}

func (t *T) update(duration time.Duration) {
	t.Lock()
	expiration := time.Now().Add(duration)
	t.expires = &expiration
	t.Unlock()
}

func (t *T) expired() bool {
	var value bool
	t.RLock()
	if t.expires == nil {
		value = true
	} else {
		value = t.expires.Before(time.Now())
	}
	t.RUnlock()
	return value
}

type Cache struct {
	mutex sync.RWMutex
	ttl   time.Duration
	data  map[string]*T
}

func NewCache(duration time.Duration) *Cache {
	cache := &Cache{
		ttl:  duration,
		data: map[string]*T{},
	}
	cache.startCleanupTimer()
	return cache
}

func (cache *Cache) Set(key string, value string) {
	cache.mutex.Lock()
	v := &T{data: value}
	v.update(cache.ttl)
	cache.data[key] = v
	cache.mutex.Unlock()
}

func (cache *Cache) Get(key string) (string, bool) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	v, exists := cache.data[key]
	if !exists || v.expired() {
		return "", false
	} else {
		return v.data, true
	}
}

func (cache *Cache) Count() int {
	cache.mutex.RLock()
	count := len(cache.data)
	cache.mutex.RUnlock()
	return count
}

func (cache *Cache) cleanup() {
	cache.mutex.Lock()
	for key, v := range cache.data {
		if v.expired() {
			delete(cache.data, key)
		}
	}
	cache.mutex.Unlock()
}

func (cache *Cache) startCleanupTimer() {
	duration := cache.ttl
	if duration < time.Second {
		duration = time.Second
	}
	go (func() {
		for _ = range time.Tick(duration) {
			cache.cleanup()
		}
	})()
}
