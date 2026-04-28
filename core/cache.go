package core

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

type CacheEntry struct {
	Data      []byte
	CreatedAt time.Time
	ExpiresAt time.Time
}

// ResponseCache is a bounded in-memory TTL cache for dedicated endpoint responses.
type ResponseCache struct {
	mu        sync.Mutex
	entries   map[string]CacheEntry
	ttl       time.Duration
	maxSize   int
	hits      int
	misses    int
	bypasses  int
	evictions int
}

func NewResponseCache(ttl time.Duration, maxSize int) *ResponseCache {
	return &ResponseCache{
		entries: make(map[string]CacheEntry),
		ttl:     ttl,
		maxSize: maxSize,
	}
}

func BuildCacheKey(engine string, action string, q Query) string {
	country, class, provider := cacheProxyMarket(q)
	raw := fmt.Sprintf(
		"%s|%s|%s|%s|%s|%s|%s|%d|%d|%t|%t|%s|%s|%s",
		engine,
		action,
		q.Text,
		q.LangCode,
		q.DateInterval,
		q.Filetype,
		q.Site,
		q.Limit,
		q.Start,
		q.Filter,
		q.Answers,
		country,
		class,
		provider,
	)
	hash := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(hash[:])
}

func cacheProxyMarket(q Query) (country string, class string, provider string) {
	country = strings.ToLower(strings.TrimSpace(q.ProxyCountry))
	if country == "" {
		// TODO: Use explicit balancer market metadata everywhere; language is only a weak market proxy.
		country = strings.ToLower(strings.TrimSpace(q.LangCode))
	}
	return country,
		strings.ToLower(strings.TrimSpace(q.ProxyClass)),
		strings.ToLower(strings.TrimSpace(q.ProxyProvider))
}

func ShouldBypassCacheForProxyMarket(q Query) bool {
	if strings.TrimSpace(q.ProxyURL) == "" && strings.TrimSpace(q.ProxyOverride) == "" {
		return false
	}
	return strings.TrimSpace(q.ProxyCountry) == "" &&
		strings.TrimSpace(q.ProxyClass) == "" &&
		strings.TrimSpace(q.ProxyProvider) == ""
}

func (c *ResponseCache) Get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pruneExpiredLocked(time.Now())

	entry, ok := c.entries[key]
	if !ok {
		c.misses++
		return nil, false
	}

	c.hits++
	return entry.Data, true
}

func (c *ResponseCache) Set(key string, data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	c.pruneExpiredLocked(now)

	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxSize {
		c.evictOldestLocked()
	}

	c.entries[key] = CacheEntry{
		Data:      data,
		CreatedAt: now,
		ExpiresAt: now.Add(c.ttl),
	}
}

func (c *ResponseCache) RecordBypass() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.bypasses++
}

func (c *ResponseCache) Stats() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.pruneExpiredLocked(time.Now())

	return map[string]interface{}{
		"status":      true,
		"entries":     len(c.entries),
		"hits":        c.hits,
		"misses":      c.misses,
		"bypasses":    c.bypasses,
		"evictions":   c.evictions,
		"ttl_seconds": int(c.ttl / time.Second),
		"max_size":    c.maxSize,
	}
}

func (c *ResponseCache) pruneExpiredLocked(now time.Time) {
	for key, entry := range c.entries {
		if !now.Before(entry.ExpiresAt) {
			delete(c.entries, key)
		}
	}
}

func (c *ResponseCache) evictOldestLocked() {
	var (
		oldestKey     string
		oldestCreated time.Time
	)

	for key, entry := range c.entries {
		if oldestKey == "" || entry.CreatedAt.Before(oldestCreated) {
			oldestKey = key
			oldestCreated = entry.CreatedAt
		}
	}

	if oldestKey != "" {
		delete(c.entries, oldestKey)
		c.evictions++
	}
}
