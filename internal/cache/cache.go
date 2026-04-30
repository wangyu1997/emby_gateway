package cache

import (
	"sync"
	"time"
)

type Item struct {
	Value      interface{}
	Expiration int64
}

type Cache struct {
	items     map[string]Item
	mu        sync.RWMutex
	defaultTTL time.Duration
	cleanupInterval time.Duration
}

func New(defaultTTL, cleanupInterval time.Duration) *Cache {
	c := &Cache{
		items:         make(map[string]Item),
		defaultTTL:    defaultTTL,
		cleanupInterval: cleanupInterval,
	}
	go c.cleanup()
	return c
}

func (c *Cache) Set(k string, v interface{}, d time.Duration) {
	var exp int64
	if d == 0 {
		d = c.defaultTTL
	}
	if d > 0 {
		exp = time.Now().Add(d).UnixNano()
	}
	c.mu.Lock()
	c.items[k] = Item{Value: v, Expiration: exp}
	c.mu.Unlock()
}

func (c *Cache) Get(k string) (interface{}, bool) {
	c.mu.RLock()
	item, found := c.items[k]
	c.mu.RUnlock()

	if !found {
		return nil, false
	}
	if item.Expiration > 0 && time.Now().UnixNano() > item.Expiration {
		return nil, false
	}
	return item.Value, true
}

func (c *Cache) cleanup() {
	ticker := time.NewTicker(c.cleanupInterval)
	defer ticker.Stop()
	for range ticker.C {
		c.mu.Lock()
		now := time.Now().UnixNano()
		for k, v := range c.items {
			if v.Expiration > 0 && now > v.Expiration {
				delete(c.items, k)
			}
		}
		c.mu.Unlock()
	}
}

func (c *Cache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.items)
}

func (c *Cache) Flush() {
	c.mu.Lock()
	c.items = make(map[string]Item)
	c.mu.Unlock()
}
