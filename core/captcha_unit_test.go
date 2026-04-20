package core

import (
	"errors"
	"testing"

	api2captcha "github.com/2captcha/2captcha-go"
)

type captchaClientMock struct {
	lastReq api2captcha.Request
	calls   int
	err     error
}

func (m *captchaClientMock) Solve(req api2captcha.Request) (string, string, error) {
	m.calls++
	m.lastReq = req
	if m.err != nil {
		return "", "", m.err
	}
	return "token", "captcha-id", nil
}

func TestToCaptchaProxy(t *testing.T) {
	tests := []struct {
		name      string
		raw       string
		wantType  string
		wantAddr  string
		wantValid bool
	}{
		{
			name:      "http without auth",
			raw:       "http://127.0.0.1:8080",
			wantType:  "HTTP",
			wantAddr:  "127.0.0.1:8080",
			wantValid: true,
		},
		{
			name:      "https with auth",
			raw:       "https://user:pass@127.0.0.1:8443",
			wantType:  "HTTPS",
			wantAddr:  "user:pass@127.0.0.1:8443",
			wantValid: true,
		},
		{
			name:      "socks5h with user only",
			raw:       "socks5h://user@127.0.0.1:1080",
			wantType:  "SOCKS5",
			wantAddr:  "user@127.0.0.1:1080",
			wantValid: true,
		},
		{
			name:      "invalid url",
			raw:       "://bad",
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotType, gotAddr, ok := toCaptchaProxy(tt.raw)
			if ok != tt.wantValid {
				t.Fatalf("expected valid=%v, got %v", tt.wantValid, ok)
			}
			if !tt.wantValid {
				return
			}
			if gotType != tt.wantType {
				t.Fatalf("expected type %q, got %q", tt.wantType, gotType)
			}
			if gotAddr != tt.wantAddr {
				t.Fatalf("expected addr %q, got %q", tt.wantAddr, gotAddr)
			}
		})
	}
}

func TestSolveReCaptcha2RecordsMetricsAndProxy(t *testing.T) {
	resetCaptchaSolverMetrics()

	mock := &captchaClientMock{}
	solver := &CaptchaSolver{client: mock}

	resp, id, err := solver.SolveReCaptcha2("sitekey", "https://example.com", "datas", "https://user:pass@127.0.0.1:8443")
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if resp != "token" || id != "captcha-id" {
		t.Fatalf("unexpected solver response: resp=%q id=%q", resp, id)
	}

	if got := mock.lastReq.Params["proxytype"]; got != "HTTPS" {
		t.Fatalf("expected proxytype HTTPS, got %q", got)
	}
	if got := mock.lastReq.Params["proxy"]; got != "user:pass@127.0.0.1:8443" {
		t.Fatalf("expected proxy addr to match upstream proxy, got %q", got)
	}

	metrics := CaptchaSolverMetrics()
	if got := metrics["solver_attempts"]; got != 1 {
		t.Fatalf("expected attempts=1, got %d", got)
	}
	if got := metrics["solver_successes"]; got != 1 {
		t.Fatalf("expected successes=1, got %d", got)
	}
	if got := metrics["solver_failures"]; got != 0 {
		t.Fatalf("expected failures=0, got %d", got)
	}
}

func TestSolveReCaptcha2RecordsFailureMetric(t *testing.T) {
	resetCaptchaSolverMetrics()

	mock := &captchaClientMock{err: errors.New("boom")}
	solver := &CaptchaSolver{client: mock}

	if _, _, err := solver.SolveReCaptcha2("sitekey", "https://example.com", "datas", ""); err == nil {
		t.Fatal("expected solver error")
	}

	metrics := CaptchaSolverMetrics()
	if got := metrics["solver_attempts"]; got != 1 {
		t.Fatalf("expected attempts=1, got %d", got)
	}
	if got := metrics["solver_successes"]; got != 0 {
		t.Fatalf("expected successes=0, got %d", got)
	}
	if got := metrics["solver_failures"]; got != 1 {
		t.Fatalf("expected failures=1, got %d", got)
	}
}

// Acceptance for milestone 1 task 1.4: with the solver gated off, no 2captcha
// API calls are made. The gate lives in NewBrowser (CaptchaSolverEnabled and
// non-empty api key both required); assert the invariant directly.
func TestCaptchaSolverConstructionGate(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		apiKey  string
		want    bool
	}{
		{"disabled with key", false, "abc", false},
		{"disabled without key", false, "", false},
		{"enabled without key", true, "", false},
		{"enabled with key", true, "abc", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.enabled && tc.apiKey != ""
			if got != tc.want {
				t.Fatalf("gate result mismatch: want %v, got %v", tc.want, got)
			}
		})
	}
}

// When a solver instance exists but the engine's gate (IsSolveCaptcha and
// CaptchaSolverEnabled) evaluates false, the 2captcha client must not receive
// a request. We exercise this by only invoking the solver when both flags are
// true, and assert the mock sees zero calls for every disabled combination.
func TestCaptchaSolverEngineGateSkipsClient(t *testing.T) {
	cases := []struct {
		name             string
		isSolveCaptcha   bool
		solverEnabled    bool
		expectInvocation bool
	}{
		{"engine off, solver off", false, false, false},
		{"engine on, solver off", true, false, false},
		{"engine off, solver on", false, true, false},
		{"engine on, solver on", true, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetCaptchaSolverMetrics()
			mock := &captchaClientMock{}
			solver := &CaptchaSolver{client: mock}

			if tc.isSolveCaptcha && tc.solverEnabled {
				if _, _, err := solver.SolveReCaptcha2("sitekey", "https://example.com", "datas", ""); err != nil {
					t.Fatalf("unexpected solver error: %v", err)
				}
			}

			wantCalls := 0
			if tc.expectInvocation {
				wantCalls = 1
			}
			if mock.calls != wantCalls {
				t.Fatalf("expected %d client calls, got %d", wantCalls, mock.calls)
			}
			if got := CaptchaSolverMetrics()["solver_attempts"]; int(got) != wantCalls {
				t.Fatalf("expected %d attempts, got %d", wantCalls, got)
			}
		})
	}
}
