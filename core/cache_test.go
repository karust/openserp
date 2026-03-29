package core

import (
	"testing"
	"time"
)

func TestResponseCacheSetAndGet(t *testing.T) {
	cache := NewResponseCache(5*time.Second, 10)
	key := BuildCacheKey("google", "search", Query{Text: "golang", Limit: 10})

	if _, ok := cache.Get(key); ok {
		t.Fatal("expected initial cache miss")
	}

	data := []byte(`[{"rank":1}]`)
	cache.Set(key, data)

	got, ok := cache.Get(key)
	if !ok {
		t.Fatal("expected cache hit")
	}
	if string(got) != string(data) {
		t.Fatalf("unexpected cached value: got %s want %s", got, data)
	}
}

func TestResponseCacheExpiration(t *testing.T) {
	cache := NewResponseCache(40*time.Millisecond, 10)
	key := BuildCacheKey("google", "search", Query{Text: "expire"})
	cache.Set(key, []byte(`[]`))

	if _, ok := cache.Get(key); !ok {
		t.Fatal("expected cache hit before expiration")
	}

	time.Sleep(60 * time.Millisecond)

	if _, ok := cache.Get(key); ok {
		t.Fatal("expected cache miss after expiration")
	}
}

func TestResponseCacheEvictsOldestEntry(t *testing.T) {
	cache := NewResponseCache(time.Minute, 2)

	firstKey := BuildCacheKey("google", "search", Query{Text: "first"})
	secondKey := BuildCacheKey("google", "search", Query{Text: "second"})
	thirdKey := BuildCacheKey("google", "search", Query{Text: "third"})

	cache.Set(firstKey, []byte(`["first"]`))
	time.Sleep(10 * time.Millisecond)
	cache.Set(secondKey, []byte(`["second"]`))
	time.Sleep(10 * time.Millisecond)
	cache.Set(thirdKey, []byte(`["third"]`))

	if _, ok := cache.Get(firstKey); ok {
		t.Fatal("expected oldest entry to be evicted")
	}
	if _, ok := cache.Get(secondKey); !ok {
		t.Fatal("expected newer entry to remain cached")
	}
	if _, ok := cache.Get(thirdKey); !ok {
		t.Fatal("expected newest entry to remain cached")
	}
}

func TestResponseCacheStats(t *testing.T) {
	cache := NewResponseCache(time.Minute, 2)
	key := BuildCacheKey("google", "search", Query{Text: "stats"})

	if _, ok := cache.Get(key); ok {
		t.Fatal("expected miss for empty cache")
	}

	cache.Set(key, []byte(`[]`))
	if _, ok := cache.Get(key); !ok {
		t.Fatal("expected cache hit")
	}
	cache.RecordBypass()

	stats := cache.Stats()
	if got := stats["status"]; got != true {
		t.Fatalf("expected enabled status, got %v", got)
	}
	if got := stats["entries"].(int); got != 1 {
		t.Fatalf("expected 1 entry, got %d", got)
	}
	if got := stats["hits"].(int); got != 1 {
		t.Fatalf("expected 1 hit, got %d", got)
	}
	if got := stats["misses"].(int); got != 1 {
		t.Fatalf("expected 1 miss, got %d", got)
	}
	if got := stats["bypasses"].(int); got != 1 {
		t.Fatalf("expected 1 bypass, got %d", got)
	}
}

func TestBuildCacheKeyChangesWithPaginationAndFlags(t *testing.T) {
	base := Query{
		Text:     "golang",
		LangCode: "EN",
		Limit:    10,
		Start:    0,
		Filter:   true,
		Answers:  false,
	}

	baseKey := BuildCacheKey("google", "search", base)

	if same := BuildCacheKey("google", "search", base); same != baseKey {
		t.Fatal("expected deterministic key for same query")
	}
	if changed := BuildCacheKey("google", "search", Query{
		Text:     "golang",
		LangCode: "EN",
		Limit:    20,
		Start:    0,
		Filter:   true,
		Answers:  false,
	}); changed == baseKey {
		t.Fatal("expected limit to affect cache key")
	}
	if changed := BuildCacheKey("google", "search", Query{
		Text:     "golang",
		LangCode: "EN",
		Limit:    10,
		Start:    10,
		Filter:   true,
		Answers:  false,
	}); changed == baseKey {
		t.Fatal("expected start to affect cache key")
	}
	if changed := BuildCacheKey("google", "search", Query{
		Text:     "golang",
		LangCode: "EN",
		Limit:    10,
		Start:    0,
		Filter:   false,
		Answers:  false,
	}); changed == baseKey {
		t.Fatal("expected filter to affect cache key")
	}
	if changed := BuildCacheKey("google", "search", Query{
		Text:     "golang",
		LangCode: "EN",
		Limit:    10,
		Start:    0,
		Filter:   true,
		Answers:  true,
	}); changed == baseKey {
		t.Fatal("expected answers to affect cache key")
	}
}
