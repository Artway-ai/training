/*
Copyright 2022 kuizhiqing.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

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

func (cache *Cache) Set(key string, value string, ttl time.Duration) {
	cache.mutex.Lock()
	expiration := time.Now().Add(ttl)
	v := &T{data: value, expires: &expiration}
	cache.data[key] = v
	cache.mutex.Unlock()
}

func (cache *Cache) SetValue(key string, value string) {
	cache.mutex.Lock()
	expiration := time.Now().Add(cache.ttl)
	v := &T{data: value, expires: &expiration}
	cache.data[key] = v
	cache.mutex.Unlock()
}

func (cache *Cache) SetTTL(key string, ttl time.Duration) {
	cache.mutex.Lock()
	expiration := time.Now().Add(ttl)
	v := &T{expires: &expiration}
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
