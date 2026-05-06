package ithelper

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/karust/openserp/core"
	"github.com/karust/openserp/testutil"
)

// HandleError skips on captcha/timeout (expected in live environments),
// fatals on other errors. In strict mode, captcha/timeout also fatal.
func HandleError(t *testing.T, operation string, err error) {
	t.Helper()
	if err == nil {
		return
	}

	if err == core.ErrCaptcha {
		t.Logf("captcha detected during %s: %v", operation, err)
		if testutil.IntegrationStrict() {
			t.Fatalf("%s failed (strict mode): %v", operation, err)
		}
		t.Skipf("skipping flaky live %s due to captcha: %v", operation, err)
	}

	if err == core.ErrSearchTimeout {
		if testutil.IntegrationStrict() {
			t.Fatalf("%s failed (strict mode): %v", operation, err)
		}
		t.Skipf("skipping flaky live %s due to timeout: %v", operation, err)
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) || core.IsContextDone(err) {
		if testutil.IntegrationStrict() {
			t.Fatalf("%s failed (strict mode): %v", operation, err)
		}
		t.Skipf("skipping flaky live %s due to context deadline: %v", operation, err)
	}

	t.Fatalf("%s failed: %v", operation, err)
}

// CreateBrowser creates a browser configured for integration tests.
// Respects OPENSERP_INTEGRATION_HEADFUL for debugging.
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
	return b
}
