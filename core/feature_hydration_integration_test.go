//go:build integration

package core

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/karust/openserp/testutil"
)

func TestFeatureHydrationWait(t *testing.T) {
	testutil.RequireIntegration(t)
	browser, err := NewBrowser(BrowserOpts{
		IsHeadless:  true,
		BrowserPath: os.Getenv("OPENSERP_APP_BROWSER_PATH"),
		Timeout:     5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { closeTestBrowser(t, browser) })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/feature" {
			if SleepContext(r.Context(), 750*time.Millisecond) == nil {
				_, _ = io.WriteString(w, "fetched")
			}
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, `<!doctype html><body style="height:4000px"></body>`)
	}))
	defer server.Close()

	for _, tc := range []struct {
		name    string
		setup   string
		want    []string
		timeout time.Duration
		minWait time.Duration
		maxWait time.Duration
	}{
		{
			name:    "already rendered",
			setup:   `() => document.body.innerHTML = '<div data-feature>ready</div>'`,
			want:    []string{"ready"},
			maxWait: 1500 * time.Millisecond,
		},
		{
			name:    "no features",
			maxWait: 1500 * time.Millisecond,
		},
		{
			name: "staggered modules",
			setup: `() => {
				const add = text => document.body.insertAdjacentHTML('beforeend', '<div data-feature>' + text + '</div>');
				setTimeout(() => add('first'), 200);
				setTimeout(() => add('second'), 500);
			}`,
			want: []string{"first", "second"},
		},
		{
			name: "pending module",
			setup: `() => {
				const module = document.body.appendChild(document.createElement('div'));
				module.setAttribute('aria-busy', 'true');
				setTimeout(() => {
					module.textContent = 'ready after busy';
					module.setAttribute('data-feature', '');
					module.removeAttribute('aria-busy');
				}, 800);
			}`,
			want: []string{"ready after busy"},
		},
		{
			name: "fetch triggered by scroll",
			setup: `() => {
				document.body.setAttribute('aria-busy', 'true');
				window.addEventListener('scroll', () => setTimeout(async () => {
					document.body.removeAttribute('aria-busy');
					const response = await fetch('/feature');
					const module = document.body.appendChild(document.createElement('div'));
					module.setAttribute('data-feature', '');
					module.textContent = await response.text();
				}, 550), {once: true});
			}`,
			want: []string{"fetched"},
		},
		{
			name:    "busy module hits cap",
			setup:   `() => document.body.innerHTML = '<div aria-busy="true"></div><div data-feature>ready</div>'`,
			want:    []string{"ready"},
			minWait: featureHydrationLimit,
			maxWait: 3 * time.Second,
		},
		{
			name:    "request deadline",
			setup:   `() => document.body.innerHTML = '<div data-feature>ready</div>'`,
			timeout: 150 * time.Millisecond,
			maxWait: time.Second,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			page, err := browser.Navigate(ctx, server.URL)
			if err != nil {
				t.Fatal(err)
			}
			defer DeferClosePage(ctx, page, browser)()
			if tc.setup != "" {
				if _, err := page.Eval(tc.setup); err != nil {
					t.Fatal(err)
				}
			}
			if tc.timeout > 0 {
				deadline, stop := context.WithTimeout(ctx, tc.timeout)
				defer stop()
				ctx = deadline
			}

			start := time.Now()
			features := FeaturesFromPageWithWait(ctx, page, func(doc *goquery.Document) []SerpFeature {
				var found []SerpFeature
				doc.Find("[data-feature]").Each(func(_ int, module *goquery.Selection) {
					found = append(found, SerpFeature{Text: module.Text()})
				})
				return found
			})
			took := time.Since(start)
			var got []string
			for _, feature := range features {
				got = append(got, feature.Text)
			}
			t.Logf("feature wait %s, content %v", took.Round(time.Millisecond), got)
			if !slices.Equal(got, tc.want) {
				t.Errorf("features = %v, want %v", got, tc.want)
			}
			if took < tc.minWait {
				t.Errorf("feature wait took %s, want at least %s", took, tc.minWait)
			}
			if tc.maxWait > 0 && took > tc.maxWait {
				t.Errorf("feature wait took %s, want at most %s", took, tc.maxWait)
			}
		})
	}
}
