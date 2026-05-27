package core

import (
	"testing"
	"time"
)

func TestCacheProxyMarketFallback(t *testing.T) {
	tests := []struct {
		name    string
		q       Query
		country string
	}{
		{name: "proxy country wins", q: Query{ProxyCountry: "DE", Region: "RU", LangCode: "EN"}, country: "de"},
		{name: "region country fallback", q: Query{Region: "RU", LangCode: "EN"}, country: "ru"},
		{name: "region locale fallback", q: Query{Region: "en-GB", LangCode: "EN"}, country: "gb"},
		{name: "lang code last resort", q: Query{LangCode: "EN"}, country: "en"},
		{name: "yandex numeric region ignored for market", q: Query{Region: "213", LangCode: "RU"}, country: "ru"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			country, _, _ := cacheProxyMarket(tt.q)
			if country != tt.country {
				t.Fatalf("cacheProxyMarket country = %q, want %q", country, tt.country)
			}
		})
	}
}

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
		Features: false,
	}

	baseKey := BuildCacheKey("google", "search", base)

	if same := BuildCacheKey("google", "search", base); same != baseKey {
		t.Fatal("expected deterministic key for same query")
	}
	if changed := BuildCacheKey("google", "search", Query{
		Text:     "golang",
		LangCode: "EN",
		Region:   "US",
		Limit:    10,
		Start:    0,
		Filter:   true,
		Features: false,
	}); changed == baseKey {
		t.Fatal("expected region to affect cache key")
	}
	if changed := BuildCacheKey("google", "search", Query{
		Text:     "golang",
		LangCode: "EN",
		Limit:    20,
		Start:    0,
		Filter:   true,
		Features: false,
	}); changed == baseKey {
		t.Fatal("expected limit to affect cache key")
	}
	if changed := BuildCacheKey("google", "search", Query{
		Text:     "golang",
		LangCode: "EN",
		Limit:    10,
		Start:    10,
		Filter:   true,
		Features: false,
	}); changed == baseKey {
		t.Fatal("expected start to affect cache key")
	}
	if changed := BuildCacheKey("google", "search", Query{
		Text:     "golang",
		LangCode: "EN",
		Limit:    10,
		Start:    0,
		Filter:   false,
		Features: false,
	}); changed == baseKey {
		t.Fatal("expected filter to affect cache key")
	}
	if changed := BuildCacheKey("google", "search", Query{
		Text:     "golang",
		LangCode: "EN",
		Limit:    10,
		Start:    0,
		Filter:   true,
		Features: true,
	}); changed == baseKey {
		t.Fatal("expected features to affect cache key")
	}
}

func TestBuildCacheKeyNormalizesStableFields(t *testing.T) {
	base := BuildCacheKey("google", "search", Query{
		Text:         " golang ",
		LangCode:     "EN",
		Region:       " us ",
		Filetype:     "PDF",
		Site:         "EXAMPLE.COM",
		Limit:        10,
		ProxyCountry: " US ",
		ProxyClass:   " Residential ",
	})
	same := BuildCacheKey("Google", "Search", Query{
		Text:         "golang",
		LangCode:     "en",
		Region:       "US",
		Filetype:     "pdf",
		Site:         "example.com",
		Limit:        10,
		ProxyCountry: "us",
		ProxyClass:   "residential",
	})
	if same != base {
		t.Fatal("expected cache key to normalize engine/action, locale, filters, and proxy market fields")
	}
}

func TestBuildCacheKeyUsesProxyMarketNotSessionOrURL(t *testing.T) {
	base := Query{
		Text:           "golang",
		LangCode:       "EN",
		Limit:          10,
		ProxyURL:       "http://user:password-a@proxy-a:8080",
		ProxyCountry:   " US ",
		ProxyClass:     " Residential ",
		ProxyProvider:  " WebShare ",
		ProxySessionID: "sid-a",
	}
	baseKey := BuildCacheKey("google", "search", base)

	sameMarket := base
	sameMarket.ProxyURL = "http://user:password-b@proxy-b:8080"
	sameMarket.ProxySessionID = "sid-b"
	if got := BuildCacheKey("google", "search", sameMarket); got != baseKey {
		t.Fatal("expected proxy URL and session id not to affect cache key")
	}

	differentCountry := base
	differentCountry.ProxyCountry = "de"
	if got := BuildCacheKey("google", "search", differentCountry); got == baseKey {
		t.Fatal("expected proxy country to affect cache key")
	}

	differentClass := base
	differentClass.ProxyClass = "datacenter"
	if got := BuildCacheKey("google", "search", differentClass); got == baseKey {
		t.Fatal("expected proxy class to affect cache key")
	}

	differentProvider := base
	differentProvider.ProxyProvider = "brightdata"
	if got := BuildCacheKey("google", "search", differentProvider); got == baseKey {
		t.Fatal("expected proxy provider to affect cache key")
	}
}

func TestBuildCacheKeyFallsBackToLanguageWhenCountryAbsent(t *testing.T) {
	base := Query{Text: "golang", LangCode: "en", Limit: 10}
	baseKey := BuildCacheKey("google", "search", base)

	changed := base
	changed.LangCode = "de"
	if got := BuildCacheKey("google", "search", changed); got == baseKey {
		t.Fatal("expected language fallback to affect cache key when proxy country is absent")
	}

	withCountry := base
	withCountry.ProxyCountry = "us"
	changedWithCountry := withCountry
	changedWithCountry.LangCode = "de"
	if got := BuildCacheKey("google", "search", changedWithCountry); got == BuildCacheKey("google", "search", withCountry) {
		t.Fatal("expected language itself to remain part of the cache key")
	}
}

func TestShouldBypassCacheForProxyMarket(t *testing.T) {
	if !ShouldBypassCacheForProxyMarket(Query{ProxyURL: "http://proxy.example:8080"}) {
		t.Fatal("expected request proxy without market metadata to bypass cache")
	}
	if !ShouldBypassCacheForProxyMarket(Query{ProxyOverride: "us"}) {
		t.Fatal("expected tag override without market metadata to bypass cache")
	}
	if ShouldBypassCacheForProxyMarket(Query{ProxyURL: "http://proxy.example:8080", ProxyCountry: "us"}) {
		t.Fatal("expected explicit country market metadata to allow cache")
	}
	if ShouldBypassCacheForProxyMarket(Query{Text: "golang"}) {
		t.Fatal("expected direct query without proxy override to allow cache")
	}
}
