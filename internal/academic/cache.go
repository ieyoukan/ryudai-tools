package academic

import (
	"sync"
	"time"
)

// ttlCache は期限つきの小さなインメモリキャッシュ。上限を超えたら期限切れ→古い順に捨てる。
type ttlCache[V any] struct {
	mu    sync.Mutex
	ttl   time.Duration
	max   int
	items map[string]cacheItem[V]
}

type cacheItem[V any] struct {
	val     V
	expires time.Time
}

func newTTLCache[V any](ttl time.Duration, max int) *ttlCache[V] {
	return &ttlCache[V]{ttl: ttl, max: max, items: make(map[string]cacheItem[V])}
}

func (c *ttlCache[V]) get(key string) (V, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	it, ok := c.items[key]
	if !ok || time.Now().After(it.expires) {
		var zero V
		return zero, false
	}
	return it.val, true
}

func (c *ttlCache[V]) set(key string, v V) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.items) >= c.max {
		now := time.Now()
		var oldestKey string
		var oldest time.Time
		for k, it := range c.items {
			if now.After(it.expires) {
				delete(c.items, k)
				continue
			}
			if oldestKey == "" || it.expires.Before(oldest) {
				oldestKey, oldest = k, it.expires
			}
		}
		if len(c.items) >= c.max && oldestKey != "" {
			delete(c.items, oldestKey)
		}
	}
	c.items[key] = cacheItem[V]{val: v, expires: time.Now().Add(c.ttl)}
}

func (c *ttlCache[V]) del(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
}

func (c *ttlCache[V]) delPrefix(prefix string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key := range c.items {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			delete(c.items, key)
		}
	}
}
