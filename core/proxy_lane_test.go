package core

import (
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"
	browserprofile "github.com/karust/openserp/core/browser"
)

func TestLaneStoreReusesCookiesBySession(t *testing.T) {
	store := NewLaneStore(10)
	key := ProxyLaneKey{Engine: "google", SessionID: "sid-a"}
	cookies := []*proto.NetworkCookie{{Name: "sid", Value: "a", Domain: "example.com", Path: "/"}}

	store.SaveCookies(key, cookies)
	got := store.Cookies(key)
	if len(got) != 1 || got[0].Name != "sid" || got[0].Value != "a" {
		t.Fatalf("expected saved cookie, got %#v", got)
	}

	other := store.Cookies(ProxyLaneKey{Engine: "google", SessionID: "sid-b"})
	if len(other) != 0 {
		t.Fatalf("expected different SID to be cookie-clean, got %#v", other)
	}
}

func TestLaneStoreDropCookiesPreservesProfile(t *testing.T) {
	store := NewLaneStore(10)
	key := ProxyLaneKey{Engine: "google", SessionID: "sid-a"}
	profile := store.Profile(key, func() browserprofile.Profile {
		return browserprofile.Profile{ID: "profile-a"}
	})
	if profile.ID != "profile-a" {
		t.Fatalf("expected initial profile, got %#v", profile)
	}

	store.SaveCookies(key, []*proto.NetworkCookie{{Name: "sid", Value: "a", Domain: "example.com", Path: "/"}})
	store.DropCookies(key)

	if got := store.Cookies(key); len(got) != 0 {
		t.Fatalf("expected cookies to be dropped, got %#v", got)
	}
	profile = store.Profile(key, func() browserprofile.Profile {
		return browserprofile.Profile{ID: "profile-b"}
	})
	if profile.ID != "profile-a" {
		t.Fatalf("expected profile to be preserved after cookie drop, got %#v", profile)
	}
	if stats := store.Stats(); stats.CookiesDropped != 1 {
		t.Fatalf("expected cookies_dropped=1, got %#v", stats)
	}
}

func TestLaneStoreEvictsLRU(t *testing.T) {
	store := NewLaneStore(2)
	keyA := ProxyLaneKey{Engine: "google", SessionID: "a"}
	keyB := ProxyLaneKey{Engine: "google", SessionID: "b"}
	keyC := ProxyLaneKey{Engine: "google", SessionID: "c"}

	store.SaveCookies(keyA, []*proto.NetworkCookie{{Name: "sid", Value: "a"}})
	time.Sleep(time.Millisecond)
	store.SaveCookies(keyB, []*proto.NetworkCookie{{Name: "sid", Value: "b"}})
	time.Sleep(time.Millisecond)
	_ = store.Cookies(keyB)
	time.Sleep(time.Millisecond)
	store.SaveCookies(keyC, []*proto.NetworkCookie{{Name: "sid", Value: "c"}})

	if got := store.Cookies(keyA); len(got) != 0 {
		t.Fatalf("expected oldest lane A to be evicted, got %#v", got)
	}
	if got := store.Cookies(keyB); len(got) != 1 {
		t.Fatalf("expected lane B to remain, got %#v", got)
	}
	if stats := store.Stats(); stats.Active != 2 || stats.EvictedLRU != 1 {
		t.Fatalf("unexpected lane stats: %#v", stats)
	}
}

func TestProxyLaneKeyForOmitsPassword(t *testing.T) {
	a := ProxyLaneKeyForTenant("Google", "", Query{}, "http://user:pass-a@proxy.example:8080")
	b := ProxyLaneKeyForTenant("google", "", Query{}, "http://user:pass-b@proxy.example:8080")
	if a.Empty() || b.Empty() {
		t.Fatalf("expected derived lane keys, got %#v %#v", a, b)
	}
	if a != b {
		t.Fatalf("expected password changes not to affect lane key: %#v %#v", a, b)
	}
}

func TestProxyLaneKeyIncludesTenant(t *testing.T) {
	q := Query{ProxySessionID: "sid-a"}
	a := ProxyLaneKeyForTenant("google", "tenant-a", q, "http://proxy.example:8080")
	b := ProxyLaneKeyForTenant("google", "tenant-b", q, "http://proxy.example:8080")
	if a == b {
		t.Fatalf("expected different tenants to produce different lane keys: %#v", a)
	}
	if got := a.ID(); got != "tenant-a:google:sid-a" {
		t.Fatalf("unexpected tenant lane id: %q", got)
	}
}
