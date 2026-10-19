package core

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

// stubRedirects swaps the network fetch and returns a reader of fetched URLs.
func stubRedirects(t *testing.T, handle func(target string) (*http.Response, error)) func() []string {
	t.Helper()
	var (
		mu    sync.Mutex
		calls []string
	)
	prev := fetchRedirect
	fetchRedirect = func(_ context.Context, target string, _ Query) (*http.Response, error) {
		mu.Lock()
		calls = append(calls, target)
		mu.Unlock()
		return handle(target)
	}
	resetResolveCooldowns()
	t.Cleanup(func() {
		fetchRedirect = prev
		resetResolveCooldowns()
	})
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		sorted := slices.Clone(calls)
		slices.Sort(sorted)
		return sorted
	}
}

func resetResolveCooldowns() {
	resolveCooldowns.Lock()
	clear(resolveCooldowns.until)
	resolveCooldowns.Unlock()
}

func httpResponse(status int, location string) *http.Response {
	resp := &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
	if location != "" {
		resp.Header.Set("Location", location)
	}
	return resp
}

// Destinations keyed by token, as Google's /goto answers them.
func googleRedirects(destinations map[string]string) func(string) (*http.Response, error) {
	return func(target string) (*http.Response, error) {
		token := target[strings.LastIndex(target, "=")+1:]
		if dest, ok := destinations[token]; ok {
			return httpResponse(http.StatusFound, dest), nil
		}
		return httpResponse(http.StatusBadRequest, ""), nil
	}
}

func TestResolveWrappedResultURLsSwapsOnlyOpaqueOrganicLinks(t *testing.T) {
	calls := stubRedirects(t, googleRedirects(map[string]string{
		"tokA": "https://www.pcmag.com/picks/vpn#:~:text=Best%20VPN",
		"tokB": "https://vpn.techlore.tech/",
		"tokC": "https://www.reddit.com/r/VPN/",
	}))
	results := []SearchResult{
		{Rank: 1, URL: "https://www.google.com/goto?url=tokA"},
		{Rank: -1, URL: "/goto?url=tokC", Type: ResultTypePeopleAlsoAsk},
		{Rank: 2, URL: "/goto?url=tokB"},
		{Rank: 3, URL: "https://example.org/clean"},
		{Rank: 4, URL: "/url?q=https%3A%2F%2Fexample.com%2Fplain"},
		{Rank: 1, Ad: true, URL: "/goto?url=tokA"},
		{Rank: 5, URL: "https://www.google.com/aclk?sa=l&ai=tokA"},
	}
	before := slices.Clone(results)

	ResolveWrappedResultURLs(context.Background(), results, Query{ResolveURLs: true}, "https://www.google.de/search?q=vpn")

	want := []string{
		"https://www.pcmag.com/picks/vpn",
		"https://www.reddit.com/r/VPN/",
		"https://vpn.techlore.tech/",
	}
	for i, url := range want {
		if results[i].URL != url {
			t.Errorf("result %d: url = %q, want %q", i, results[i].URL, url)
		}
	}
	for i := len(want); i < len(results); i++ {
		if results[i].URL != before[i].URL {
			t.Errorf("result %d must stay untouched: %q -> %q", i, before[i].URL, results[i].URL)
		}
	}
	wantCalls := []string{
		"https://www.google.com/goto?url=tokA",
		"https://www.google.de/goto?url=tokB",
		"https://www.google.de/goto?url=tokC",
	}
	if got := calls(); !slices.Equal(got, wantCalls) {
		t.Errorf("fetched %v, want %v", got, wantCalls)
	}
}

func TestResolvableGoogleRedirect(t *testing.T) {
	for _, tc := range []struct {
		raw, page, want string
	}{
		{"/goto?url=token", "https://www.google.de/search?q=x", "https://www.google.de/goto?url=token"},
		{"//www.google.de/goto?url=token", "https://www.google.com/search?q=x", "https://www.google.de/goto?url=token"},
		{"//www.google.com/goto?url=token", "", "https://www.google.com/goto?url=token"},
		{"/goto?url=token", "ftp://www.google.com/search", "https://www.google.com/goto?url=token"},
		{"/goto?url=token", "https://other.example/search", "https://www.google.com/goto?url=token"},
		{"ftp://www.google.com/goto?url=token", "", ""},
		{"https:/goto?url=token", "", ""},
		{"https://google.example.com/goto?url=token", "", ""},
		{"https://www.google.com/aclk?url=token", "", ""},
	} {
		got, ok := resolvableGoogleRedirect(tc.raw, tc.page)
		if got != tc.want || ok != (tc.want != "") {
			t.Errorf("resolvableGoogleRedirect(%q, %q) = %q, %v; want %q", tc.raw, tc.page, got, ok, tc.want)
		}
	}
}

func TestResolveCooldownPrunesUnusedExits(t *testing.T) {
	resetResolveCooldowns()
	t.Cleanup(resetResolveCooldowns)
	const resolveCooldown = time.Minute
	now := time.Now()
	startResolveCooldown("expired", now.Add(-resolveCooldown), resolveCooldown)
	startResolveCooldown("active", now, resolveCooldown)

	if resolveCoolingDown("unseen", now) || !resolveCoolingDown("active", now) {
		t.Fatal("cooldown must only affect active exits")
	}
	resolveCooldowns.Lock()
	_, expired := resolveCooldowns.until["expired"]
	resolveCooldowns.Unlock()
	if expired {
		t.Fatal("expired exit retained after an unrelated lookup")
	}
	if resolveCoolingDown("active", now.Add(resolveCooldown)) {
		t.Fatal("cooldown must expire at its deadline")
	}
}

func TestResolveWrappedResultURLsCanceled(t *testing.T) {
	calls := stubRedirects(t, googleRedirects(map[string]string{"tok": "https://a.example/"}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	results := []SearchResult{{Rank: 1, URL: "/goto?url=tok"}}

	ResolveWrappedResultURLs(ctx, results, Query{ResolveURLs: true}, "")

	if len(calls()) != 0 || results[0].URL != "/goto?url=tok" {
		t.Fatalf("canceled resolve fetched %v, url=%q", calls(), results[0].URL)
	}
}

func TestResolveWrappedResultURLsTopCountsOrganicPositions(t *testing.T) {
	calls := stubRedirects(t, googleRedirects(map[string]string{"tok1": "https://a.example/", "tok3": "https://c.example/"}))
	results := []SearchResult{
		{Rank: 1, URL: "/goto?url=tok1"},
		{Rank: -1, URL: "/goto?url=paa"},
		{Rank: 2, URL: "https://b.example/"},
		{Rank: 3, URL: "/goto?url=tok3"},
	}

	ResolveWrappedResultURLs(context.Background(), results, Query{ResolveURLs: true, ResolveTop: 2}, "")

	if got := calls(); !slices.Equal(got, []string{"https://www.google.com/goto?url=tok1"}) {
		t.Errorf("fetched %v, want only the first organic wrapper", got)
	}
	if results[0].URL != "https://a.example/" || results[3].URL != "/goto?url=tok3" {
		t.Errorf("unexpected urls: %q, %q", results[0].URL, results[3].URL)
	}
}

func TestResolveWrappedResultURLsKeepsWrapperOnFailure(t *testing.T) {
	stubRedirects(t, func(target string) (*http.Response, error) {
		switch {
		case strings.HasSuffix(target, "=net"):
			return nil, errors.New("connection reset")
		case strings.HasSuffix(target, "=ok200"):
			return httpResponse(http.StatusOK, ""), nil
		case strings.HasSuffix(target, "=nested"):
			return httpResponse(http.StatusFound, "https://www.google.com/url?q=other-token"), nil
		case strings.HasSuffix(target, "=scheme"):
			return httpResponse(http.StatusFound, "javascript:alert(1)"), nil
		}
		return httpResponse(http.StatusBadRequest, ""), nil
	})
	raw := []string{"/goto?url=net", "/goto?url=ok200", "/goto?url=nested", "/goto?url=scheme", "/goto?url=bad"}
	results := make([]SearchResult, len(raw))
	for i, u := range raw {
		results[i] = SearchResult{Rank: i + 1, URL: u}
	}

	ResolveWrappedResultURLs(context.Background(), results, Query{ResolveURLs: true}, "")

	for i, u := range raw {
		if results[i].URL != u {
			t.Errorf("%s: rewritten to %q", u, results[i].URL)
		}
	}
	if resolveCoolingDown("", time.Now()) {
		t.Error("ordinary failures must not pause resolving")
	}
}

func TestResolveWrappedResultURLsBacksOffWhenChallenged(t *testing.T) {
	for name, challenge := range map[string]*http.Response{
		"429":     httpResponse(http.StatusTooManyRequests, ""),
		"403":     httpResponse(http.StatusForbidden, ""),
		"sorry":   httpResponse(http.StatusFound, "https://www.google.com/sorry/index?continue=x"),
		"consent": httpResponse(http.StatusFound, "https://consent.google.de/ml?continue=x"),
	} {
		t.Run(name, func(t *testing.T) {
			calls := stubRedirects(t, func(string) (*http.Response, error) { return challenge, nil })
			query := Query{ResolveURLs: true, ProxyURL: "http://proxy-a.example:8080"}
			results := []SearchResult{{Rank: 1, URL: "/goto?url=tok"}}

			ResolveWrappedResultURLs(context.Background(), results, query, "")
			if results[0].URL != "/goto?url=tok" {
				t.Fatalf("challenged resolve rewrote url to %q", results[0].URL)
			}
			ResolveWrappedResultURLs(context.Background(), results, query, "")
			if got := len(calls()); got != 1 {
				t.Fatalf("fetched %d times, want the second search to skip resolving", got)
			}

			// A different exit has its own reputation.
			query.ProxyURL = "http://proxy-b.example:8080"
			ResolveWrappedResultURLs(context.Background(), results, query, "")
			if got := len(calls()); got != 2 {
				t.Errorf("fetched %d times, want the other exit to keep resolving", got)
			}
		})
	}
}

func TestResolveWrappedResultURLsDisabled(t *testing.T) {
	calls := stubRedirects(t, googleRedirects(map[string]string{"tok": "https://a.example/"}))
	results := []SearchResult{{Rank: 1, URL: "/goto?url=tok"}}

	ResolveWrappedResultURLs(context.Background(), results, Query{}, "")

	if len(calls()) != 0 || results[0].URL != "/goto?url=tok" {
		t.Errorf("resolve ran while disabled: calls=%v url=%q", calls(), results[0].URL)
	}
}

func TestStripTextFragmentDirective(t *testing.T) {
	cases := map[string]string{
		"https://a.example/p#:~:text=Best%20VPN":       "https://a.example/p",
		"https://a.example/p#faq:~:text=Best":          "https://a.example/p#faq",
		"https://a.example/p#faq":                      "https://a.example/p#faq",
		"https://a.example/p?q=1":                      "https://a.example/p?q=1",
		"https://a.example/p#:~:text=a&text=b":         "https://a.example/p",
		"https://a.example/p?srsltid=x#:~:text=Cheap%": "https://a.example/p?srsltid=x",
	}
	for in, want := range cases {
		if got := stripTextFragmentDirective(in); got != want {
			t.Errorf("stripTextFragmentDirective(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestParseResolveURLs(t *testing.T) {
	cases := []struct {
		raw     string
		enabled bool
		top     int
		wantErr bool
	}{
		{"", true, 0, false},
		{"true", true, 0, false},
		{"TRUE", true, 0, false},
		{"false", false, 0, false},
		{"0", false, 0, false},
		{"1", true, 1, false},
		{" 3 ", true, 3, false},
		{"-1", false, 0, true},
		{"all", false, 0, true},
	}
	for _, tc := range cases {
		enabled, top, err := ParseResolveURLs(tc.raw)
		if (err != nil) != tc.wantErr || enabled != tc.enabled || top != tc.top {
			t.Errorf("ParseResolveURLs(%q) = %v, %d, %v; want %v, %d, err=%v",
				tc.raw, enabled, top, err, tc.enabled, tc.top, tc.wantErr)
		}
	}
}

func TestInitFromContextResolveURLs(t *testing.T) {
	app := fiber.New()
	app.Get("/probe", func(c *fiber.Ctx) error {
		q := Query{}
		if err := q.InitFromContext(c); err != nil {
			return c.Status(http.StatusBadRequest).SendString(err.Error())
		}
		if !q.ResolveURLs || q.ResolveTop != 0 {
			return c.Status(http.StatusTeapot).SendString(q.String())
		}
		return c.SendStatus(http.StatusOK)
	})
	for query, want := range map[string]int{
		"?text=q":                  http.StatusOK,
		"?text=q&resolve_urls=yes": http.StatusBadRequest,
	} {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/probe"+query, nil), -1)
		if err != nil {
			t.Fatalf("%s: %v", query, err)
		}
		if resp.StatusCode != want {
			t.Errorf("%s: status %d, want %d", query, resp.StatusCode, want)
		}
	}
}

func TestServerGoogleResolveDefaultsAndOverrides(t *testing.T) {
	for _, tc := range []struct {
		setting, query string
		enabled        bool
		top            int
	}{
		{"false", "", false, 0},
		{"2", "", true, 2},
		{"false", "&resolve_urls=true", true, 0},
		{"2", "&resolve_urls=false", false, 0},
		{"false", "&resolve_urls=3", true, 3},
	} {
		for _, path := range []string{"/google/search?text=q", "/mega/search?text=q"} {
			t.Run(tc.setting+path+tc.query, func(t *testing.T) {
				opts := DefaultServerOptions()
				opts.CacheTTL = 0
				opts.GoogleResolve.URLs = tc.setting
				received := make(chan Query, 1)
				engine := &engineMock{name: "google", initialized: true, searchFn: func(_ context.Context, q Query) ([]SearchResult, error) {
					received <- q
					return []SearchResult{{Rank: 1, URL: "https://example.org/", Title: "Result"}}, nil
				}}
				srv := NewServerWithOptions("127.0.0.1", 0, opts, engine)
				resp := request(t, srv, path+tc.query)
				DrainAndCloseResponse(resp)
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("unexpected status: %d", resp.StatusCode)
				}
				q := <-received
				if q.ResolveURLs != tc.enabled || q.ResolveTop != tc.top || q.ResolveConfig == nil || *q.ResolveConfig != opts.GoogleResolve {
					t.Fatalf("unexpected resolution settings: %s, config=%+v", q.String(), q.ResolveConfig)
				}
			})
		}
	}
}

func TestResolveWrappedResultURLsUsesConfiguredLimits(t *testing.T) {
	calls := stubRedirects(t, func(string) (*http.Response, error) {
		return httpResponse(http.StatusForbidden, ""), nil
	})
	cfg := DefaultURLResolveConfig()
	cfg.Concurrency, cfg.Cooldown = 1, time.Second
	results := []SearchResult{{Rank: 1, URL: "/goto?url=one"}, {Rank: 2, URL: "/goto?url=two"}}
	ResolveWrappedResultURLs(context.Background(), results, Query{ResolveURLs: true, ResolveConfig: &cfg}, "")
	if len(calls()) != 1 {
		t.Fatalf("expected serial resolution to stop at first challenge, got %v", calls())
	}
	if resolveCoolingDown("", time.Now().Add(cfg.Cooldown)) {
		t.Fatal("configured cooldown did not expire")
	}
	fetchRedirect = func(ctx context.Context, _ string, _ Query) (*http.Response, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	cfg.Timeout = 10 * time.Millisecond
	started := time.Now()
	ResolveWrappedResultURLs(context.Background(), results, Query{ResolveURLs: true, ResolveConfig: &cfg}, "")
	if time.Since(started) > time.Second {
		t.Fatal("configured resolution budget was ignored")
	}
}
