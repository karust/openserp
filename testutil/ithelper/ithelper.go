package ithelper

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
)

// flakyLiveErrors are failure modes expected when hitting live engines from
// arbitrary IPs (captcha walls, IP blocks, rate limits, slow pages). They do
// not indicate broken code, so tests skip instead of failing unless
// OPENSERP_INTEGRATION_STRICT is set.
var flakyLiveErrors = []error{
	core.ErrCaptcha,
	core.ErrBlocked,
	core.ErrRateLimited,
	core.ErrSearchTimeout,
	context.DeadlineExceeded,
	context.Canceled,
}

// HandleError skips on captcha/block/rate-limit/timeout (expected against
// live engines), fatals on other errors. In strict mode everything fatals.
func HandleError(t *testing.T, operation string, err error) {
	t.Helper()
	if err == nil {
		return
	}

	flaky := core.IsContextDone(err)
	for _, sentinel := range flakyLiveErrors {
		if errors.Is(err, sentinel) {
			flaky = true
			break
		}
	}
	if !flaky {
		t.Fatalf("%s failed: %v", operation, err)
	}
	if testutil.IntegrationStrict() {
		t.Fatalf("%s failed (strict mode): %v", operation, err)
	}
	t.Skipf("skipping flaky live %s: %v", operation, err)
}

// EngineOptions returns engine options tuned for live-site integration runs.
// Real SERPs (Baidu especially) hydrate result cards client-side and can take
// well past the 5s default selector timeout on a first visit.
func EngineOptions() core.SearchEngineOptions {
	return core.SearchEngineOptions{SelectorTimeout: 15}
}

// CreateBrowser creates a browser configured for integration tests and closes
// it when the test finishes. Respects OPENSERP_INTEGRATION_HEADFUL for
// debugging (browser and page are left open for inspection).
func CreateBrowser(t *testing.T) *core.Browser {
	t.Helper()
	headful := testutil.IntegrationHeadful()
	opts := core.BrowserOpts{
		IsHeadless:    !headful,
		IsLeakless:    false,
		Timeout:       time.Second * 30,
		LeavePageOpen: headful,
	}
	b, err := core.NewBrowser(opts)
	if err != nil {
		t.Fatalf("failed to create test browser: %v", err)
	}
	if !headful {
		t.Cleanup(func() {
			if cerr := b.Close(); cerr != nil {
				t.Logf("close test browser: %v", cerr)
			}
		})
	}
	return b
}

// RunEngineTests runs the live web + image search smoke checks shared by every
// engine's integration test: results come back, and the first one has a URL
// and a title. newEngine receives a fresh browser per subtest.
func RunEngineTests(t *testing.T, newEngine func(*core.Browser) core.SearchEngine) {
	testutil.RequireIntegration(t)

	t.Run("web", func(t *testing.T) {
		engine := newEngine(CreateBrowser(t))
		query := core.Query{Text: "golang programming", Limit: 10}
		results, err := engine.Search(context.Background(), query)
		HandleError(t, engine.Name()+" web search", err)
		requireResults(t, results)
	})

	t.Run("image", func(t *testing.T) {
		engine := newEngine(CreateBrowser(t))
		query := core.Query{Text: "golden retriever puppy", Limit: 10}
		results, err := engine.SearchImage(context.Background(), query)
		HandleError(t, engine.Name()+" image search", err)
		requireResults(t, results)
	})
}

func requireResults(t *testing.T, results []core.SearchResult) {
	t.Helper()
	if len(results) == 0 {
		t.Fatal("returned empty results")
	}
	if results[0].URL == "" {
		t.Fatal("first result URL is empty")
	}
	if results[0].Title == "" {
		t.Fatal("first result title is empty")
	}
}
