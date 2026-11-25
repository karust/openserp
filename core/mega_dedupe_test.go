package core

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
)

func TestMegaSearchDeduplicatesHTTPAndHTTPS(t *testing.T) {
	const (
		insecureURL = "http://www.privoxy.org/"
		secureURL   = "https://www.privoxy.org/"
	)

	google := &engineMock{
		name:        "google",
		initialized: true,
		searchFn: func(context.Context, Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 2, URL: secureURL, Title: "Privoxy - Home Page"}}, nil
		},
	}
	duck := &engineMock{
		name:        "duckduckgo",
		initialized: true,
		searchFn: func(context.Context, Query) ([]SearchResult, error) {
			return []SearchResult{{Rank: 1, URL: insecureURL, Title: "Privoxy"}}, nil
		},
	}
	google.imageFn = google.searchFn
	duck.imageFn = duck.searchFn
	opts := DefaultServerOptions()
	opts.Resilience.Retry.MaxRetries = 0
	opts.CacheTTL = 0
	srv := NewServerWithOptions("127.0.0.1", 7209, opts, google, duck)
	secure := ClusterOccurrence{Engine: google.name, Rank: 2, ResultID: "s_b5ece77eae76b584"}
	insecure := ClusterOccurrence{Engine: duck.name, Rank: 1, ResultID: "s_e22e5bc61e5d183a"}

	for _, tc := range []struct {
		name        string
		querySuffix string
		urls        []string
		canonical   string
		occurrences []ClusterOccurrence
	}{
		{
			name:        "default dedupe",
			urls:        []string{secureURL},
			canonical:   secureURL,
			occurrences: []ClusterOccurrence{secure, insecure},
		},
		{
			name:        "dedupe disabled",
			querySuffix: "&dedupe=false",
			urls:        []string{insecureURL, secureURL},
			canonical:   secureURL,
			occurrences: []ClusterOccurrence{secure, insecure},
		},
		{
			name:        "HTTP only",
			querySuffix: "&engines=duckduckgo",
			urls:        []string{insecureURL},
			canonical:   insecureURL,
			occurrences: []ClusterOccurrence{insecure},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := request(t, srv, "/mega/search?text=privoxy"+tc.querySuffix)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status %d", resp.StatusCode)
			}
			var env Envelope
			if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
				t.Fatal(err)
			}

			var urls []string
			for _, result := range env.Results {
				urls = append(urls, result.URL)
				want := insecure
				if result.URL == secureURL {
					want = secure
				}
				if result.ID != want.ResultID || result.Engine != want.Engine || result.Rank != want.Rank {
					t.Errorf("result %+v does not retain original occurrence %+v", result, want)
				}
			}
			slices.Sort(urls)
			if !slices.Equal(urls, tc.urls) {
				t.Errorf("result URLs %v, want %v", urls, tc.urls)
			}
			if env.Clusters == nil || len(*env.Clusters) != 1 {
				t.Fatalf("expected one cluster, got %+v", env.Clusters)
			}
			cluster := (*env.Clusters)[0]
			if cluster.CanonicalURL != tc.canonical || cluster.ID != buildClusterID(tc.canonical) || cluster.BestRank != 1 {
				t.Errorf("wrong canonical URL, ID or best rank in cluster %+v", cluster)
			}
			if cluster.EnginesCount != len(tc.occurrences) || len(cluster.Occurrences) != len(tc.occurrences) {
				t.Fatalf("incorrect cluster counts in %+v", cluster)
			}
			for _, want := range tc.occurrences {
				if !slices.Contains(cluster.Occurrences, want) {
					t.Errorf("missing occurrence %+v in %+v", want, cluster.Occurrences)
				}
			}
		})
	}

	t.Run("image retains both URLs", func(t *testing.T) {
		resp := request(t, srv, "/mega/image?text=privoxy")
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d", resp.StatusCode)
		}
		var env ImageEnvelope
		if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
			t.Fatal(err)
		}
		var urls []string
		for _, result := range env.Results {
			urls = append(urls, result.Image.URL)
		}
		slices.Sort(urls)
		if want := []string{insecureURL, secureURL}; !slices.Equal(urls, want) {
			t.Errorf("image URLs %v, want %v", urls, want)
		}
	})
}

func TestMegaWebURLKeys(t *testing.T) {
	for _, tc := range []struct {
		name   string
		first  string
		second string
		ad     bool
		paired bool
	}{
		{name: "explicit default ports", first: "http://example.com:80/a?x=1", second: "https://example.com:443/a?x=1", paired: true},
		{name: "non-default port", first: "http://example.com:443/a", second: "https://example.com:443/a"},
		{name: "same scheme", first: "http://example.com:80/a", second: "http://example.com/a"},
		{name: "different path", first: "http://example.com/a", second: "https://example.com/b"},
		{name: "different query", first: "http://example.com/a?x=1", second: "https://example.com/a?x=2"},
		{name: "credentials", first: "http://user@example.com/a", second: "https://user@example.com/a"},
		{name: "opaque Google redirect", first: "http://www.google.com/goto?t=abc", second: "https://www.google.com/goto?t=abc"},
		{name: "Bing player", first: "http://www.bing.com/videos/riverview/relatedvideo?mid=1", second: "https://www.bing.com/videos/riverview/relatedvideo?mid=1"},
		{name: "different result kind", first: "http://example.com/a", second: "https://example.com/a", ad: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			urls := []megaURLKey{{url: tc.first}, {url: tc.second, ad: tc.ad}}
			keys := megaWebURLKeys(urls)
			if tc.paired {
				want := megaURLKey{url: "https://example.com/a?x=1"}
				if keys[0] != want || keys[1] != want {
					t.Errorf("keys %v, want %v", keys, want)
				}
			} else if !slices.Equal(keys, urls) {
				t.Errorf("keys %v, want original %v", keys, urls)
			}
		})
	}
}

func TestBuildClustersPairsWithinResultKind(t *testing.T) {
	httpResult := Result{Engine: "google", URL: "http://example.com:80/", Rank: 1, Type: ResultTypeOrganic}
	httpsResult := Result{Engine: "google", URL: "https://example.com:443/", Rank: 2, Type: ResultTypeAd}
	if got := BuildClusters([]Result{httpResult, httpsResult}, 1); len(got) != 2 {
		t.Errorf("different result kinds should retain separate clusters: %+v", got)
	}

	ad := httpResult
	ad.Engine, ad.Type = "bing", ResultTypeAd
	if clusters := BuildClusters([]Result{httpResult, ad}, 2); len(clusters) != 1 {
		t.Errorf("exact URLs should still share a cluster: %+v", clusters)
	}

	httpsResult.Type = ResultTypeOrganic
	clusters := BuildClusters([]Result{httpResult, httpsResult}, 1)
	if len(clusters) != 1 {
		t.Fatalf("expected one same-kind cluster, got %+v", clusters)
	}
	got := clusters[0]
	if got.EnginesCount != 1 || len(got.Occurrences) != 2 || got.CanonicalURL != httpsResult.URL || got.ID != buildClusterID("https://example.com/") {
		t.Errorf("unexpected same-engine cluster %+v", got)
	}
}

func TestMegaSearchPreservesEngineFeaturesDuringDedupe(t *testing.T) {
	const httpURL = "http://example.org"
	const httpsURL = "https://example.org/"
	google := &engineMock{name: "google", initialized: true, searchFn: func(context.Context, Query) ([]SearchResult, error) {
		return AttachFeaturesToFirstResult([]SearchResult{{Rank: 1, URL: httpURL}}, []SerpFeature{{Type: ResultTypeWeather}}), nil
	}}
	bing := &engineMock{name: "bing", initialized: true, searchFn: func(context.Context, Query) ([]SearchResult, error) {
		return []SearchResult{{Rank: 2, URL: httpsURL}}, nil
	}}
	duck := &engineMock{name: "duckduckgo", initialized: true, searchFn: func(context.Context, Query) ([]SearchResult, error) {
		return AttachFeaturesToFirstResult(nil, []SerpFeature{{Type: ResultTypeCalculator}}), nil
	}}
	opts := DefaultServerOptions()
	opts.CacheTTL = 0
	srv := NewServerWithOptions("127.0.0.1", 0, opts, google, bing, duck)
	resp := request(t, srv, "/mega/search?text=weather&features=true")
	defer resp.Body.Close()
	var env Envelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if len(env.Results) != 1 || env.Results[0].URL != httpsURL {
		t.Errorf("expected one HTTPS homepage, got %+v", env.Results)
	}
	if env.Clusters == nil || len(*env.Clusters) != 1 || len((*env.Clusters)[0].Occurrences) != 2 {
		t.Errorf("expected one cluster covering both homepages, got %+v", env.Clusters)
	}
	features := make(map[string]SerpFeature)
	for _, feature := range env.SerpFeatures {
		features[feature.Engine] = feature
	}
	if len(env.SerpFeatures) != 2 {
		t.Errorf("expected both engines' features, got %+v", env.SerpFeatures)
	}
	if f := features["google"]; f.Type != ResultTypeWeather || !slices.Equal(f.SourceResultIDs, []string{ResultID("google", httpURL)}) {
		t.Errorf("incorrect Google feature attribution: %+v", f)
	}
	if f := features["duckduckgo"]; f.Type != ResultTypeCalculator || len(f.SourceResultIDs) != 0 {
		t.Errorf("incorrect standalone feature attribution: %+v", f)
	}
}

func TestMegaClusterRepresentativeOnTiedRanks(t *testing.T) {
	bing := Result{Engine: "bing", Rank: 1, URL: "https://example.org/", Title: "Bing title"}
	google := Result{Engine: "google", Rank: 1, URL: bing.URL, Title: "Google title"}
	for _, rows := range [][]Result{{bing, google}, {google, bing}} {
		clusters := BuildClusters(rows, 2)
		if len(clusters) != 1 || clusters[0].Title != bing.Title {
			t.Fatalf("canonical result depends on engine completion order: %+v", clusters)
		}
	}
}

func TestMegaResultOrderKeepsExplicitAdType(t *testing.T) {
	rows := []MegaSearchResult{
		{Engine: "bing", SearchResult: SearchResult{Rank: 1, URL: "http://example.org/"}},
		{Engine: "google", SearchResult: SearchResult{Rank: 2, URL: "https://example.org/", Type: ResultTypeAd}},
	}
	if got := megaResultOrder(rows, true, true); !slices.Equal(got, []int{0, 1}) {
		t.Fatalf("expected separate ad and organic rows, got indices %v", got)
	}
}
