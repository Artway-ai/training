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
	"testing"
	"time"
)

func TestAPIs(t *testing.T) {
	cacheKey := "key"

	cache := NewCache(time.Second * 3)
	cache.SetTTL(cacheKey, time.Second*2)
	if _, exists := cache.Get(cacheKey); !exists {
		t.Errorf("not found")
	}
	time.Sleep(time.Second * 3)
	if _, exists := cache.Get(cacheKey); exists {
		t.Errorf("found")
	}
}
