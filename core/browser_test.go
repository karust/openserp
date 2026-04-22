//go:build integration
// +build integration

package core

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-rod/rod"
	"github.com/karust/openserp/testutil"
)

const botFingerprintTestsEnv = "OPENSERP_BOT_TESTS"
const botFingerprintArtifactDir = "testdata"

var criticalSannysoftChecks = []string{"webdriver"}

const sannysoftURL = "https://bot.sannysoft.com"

func TestCreateBrowser(t *testing.T) {
	testutil.RequireIntegration(t)

	opts := BrowserOpts{IsHeadless: true, IsLeakless: false}
	browser, err := NewBrowser(opts)
	if err != nil {
		t.Fatalf("Error failed initializing browser: %s", err)
	}

	page, err := browser.Navigate(context.Background(), "about:blank")
	if err != nil {
		t.Fatalf("navigate about:blank: %v", err)
	}
	defer closeTestBrowser(t, browser)
	defer func() {
		if err := ClosePageWithTimeout(context.Background(), page, time.Second); err != nil {
			t.Logf("close page: %v", err)
		}
	}()
}

func TestNavigateUsesIsolatedBrowserContext(t *testing.T) {
	testutil.RequireIntegration(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cookies/set":
			http.SetCookie(w, &http.Cookie{
				Name:  "openserp_session",
				Value: "request-a",
				Path:  "/",
			})
			_, _ = w.Write([]byte("cookie-set"))
		case "/cookies":
			_, _ = w.Write([]byte(r.Header.Get("Cookie")))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	opts := BrowserOpts{IsHeadless: true, IsLeakless: false, Timeout: 15 * time.Second}
	browser, err := NewBrowser(opts)
	if err != nil {
		t.Fatalf("failed initializing browser: %s", err)
	}
	defer closeTestBrowser(t, browser)

	pageA, err := browser.Navigate(context.Background(), srv.URL+"/cookies/set")
	if err != nil {
		t.Fatalf("navigate cookie setter: %v", err)
	}
	if err := ClosePageWithTimeout(context.Background(), pageA, time.Second); err != nil {
		t.Fatalf("close setter page: %v", err)
	}

	pageB, err := browser.Navigate(context.Background(), srv.URL+"/cookies")
	if err != nil {
		t.Fatalf("navigate cookie reader: %v", err)
	}
	defer func() {
		if err := ClosePageWithTimeout(context.Background(), pageB, time.Second); err != nil {
			t.Logf("close reader page: %v", err)
		}
	}()

	body, err := pageB.Timeout(5 * time.Second).Element("body")
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	cookieHeader, err := body.Text()
	if err != nil {
		t.Fatalf("extract response text: %v", err)
	}

	if strings.Contains(cookieHeader, "openserp_session=request-a") {
		t.Fatalf("cookie leaked between requests; got header %q", cookieHeader)
	}
}

func TestFingerprintSannysoft(t *testing.T) {
	testutil.RequireIntegration(t)
	if strings.TrimSpace(os.Getenv(botFingerprintTestsEnv)) != "1" {
		t.Skipf("set %s=1 to run fingerprint tests", botFingerprintTestsEnv)
	}

	stealthOn := runSannysoftFingerprint(t, true)
	stealthOff := runSannysoftFingerprint(t, false)

	improved, regressed, stillDetected := compareSannysoftRuns(stealthOff, stealthOn)
	t.Logf("Stealth comparison (OFF -> ON): improved=%d regressed=%d still_detected=%d", len(improved), len(regressed), len(stillDetected))
	if len(improved) > 0 {
		t.Logf("Stealth fixed: %s", strings.Join(improved, ", "))
	}
	if len(regressed) > 0 {
		t.Logf("WARN: stealth regressions: %s", strings.Join(regressed, ", "))
	}
	if len(stillDetected) > 0 {
		t.Logf("WARN: checks still detected with stealth: %s", strings.Join(stillDetected, ", "))
	}

	reportPath := filepath.Join(botFingerprintArtifactDir, "fingerprint_sannysoft_report.json")
	report := sannysoftReport{
		GeneratedAtUTC: time.Now().UTC().Format(time.RFC3339),
		URL:            sannysoftURL,
		Runs:           []sannysoftRunSummary{stealthOff, stealthOn},
		Comparison: sannysoftComparison{
			Baseline:      stealthModeLabel(false),
			Candidate:     stealthModeLabel(true),
			Improved:      improved,
			Regressed:     regressed,
			StillDetected: stillDetected,
		},
	}
	if err := writeSannysoftReport(reportPath, report); err != nil {
		t.Fatalf("write sannysoft report: %v", err)
	}
	absReportPath, err := filepath.Abs(reportPath)
	if err == nil {
		t.Logf("Sannysoft report artifact: %s", absReportPath)
	} else {
		t.Logf("Sannysoft report artifact: core/%s", filepath.ToSlash(reportPath))
	}

	for _, run := range []sannysoftRunSummary{stealthOff, stealthOn} {
		if len(run.CriticalFailures) > 0 {
			t.Errorf("CRITICAL fingerprint failures (%s): %v", stealthModeLabel(run.UseStealth), run.CriticalFailures)
		}
	}
}

type sannysoftCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
}

type sannysoftRunSummary struct {
	UseStealth       bool                      `json:"use_stealth"`
	ScreenshotPath   string                    `json:"screenshot_path"`
	Checks           []sannysoftCheck          `json:"checks"`
	Passed           int                       `json:"passed"`
	Failed           int                       `json:"failed"`
	Unknown          int                       `json:"unknown"`
	FailedChecks     []string                  `json:"failed_checks"`
	CriticalFailures []string                  `json:"critical_failures"`
	ChecksByName     map[string]sannysoftCheck `json:"-"`
}

func runSannysoftFingerprint(t *testing.T, useStealth bool) sannysoftRunSummary {
	t.Helper()

	label := stealthModeLabel(useStealth)

	opts := BrowserOpts{
		IsHeadless: true,
		IsLeakless: false,
		Timeout:    15 * time.Second,
		UseStealth: useStealth,
	}
	browser, err := NewBrowser(opts)
	if err != nil {
		t.Fatalf("create browser (%s): %v", label, err)
	}
	defer closeTestBrowser(t, browser)

	artifactPath := filepath.Join(botFingerprintArtifactDir, fmt.Sprintf("fingerprint_sannysoft_%s.png", label))

	page, err := browser.Navigate(context.Background(), sannysoftURL)
	if err != nil {
		t.Fatalf("navigate to sannysoft (%s): %v", label, err)
	}
	defer func() {
		if err := ClosePageWithTimeout(context.Background(), page, time.Second); err != nil {
			t.Logf("close page (%s): %v", label, err)
		}
	}()
	// Keep screenshot defer after page close defer so it runs first (LIFO).
	defer saveSannysoftScreenshot(t, page, artifactPath, label)

	if err := waitForSannysoftResults(page, 20*time.Second); err != nil {
		t.Fatalf("waiting for sannysoft results (%s): %v", label, err)
	}

	checks, err := extractSannysoftResults(page)
	if err != nil {
		t.Fatalf("extracting sannysoft results (%s): %v", label, err)
	}
	if len(checks) == 0 {
		t.Fatalf("sannysoft did not return any fingerprint check rows (%s)", label)
	}

	summary := sannysoftRunSummary{
		UseStealth:     useStealth,
		ScreenshotPath: filepath.ToSlash(filepath.Join("core", artifactPath)),
		ChecksByName:   make(map[string]sannysoftCheck, len(checks)),
		Checks:         make([]sannysoftCheck, 0, len(checks)),
	}

	for _, check := range checks {
		summary.Checks = append(summary.Checks, check)

		switch check.Status {
		case "pass":
			summary.Passed++
		case "fail":
			summary.Failed++
			t.Logf("WARN (%s): detected by: %s", label, check.Name)
			summary.FailedChecks = append(summary.FailedChecks, check.Name)
			if isCriticalFingerprintFailure(check.Name) {
				summary.CriticalFailures = append(summary.CriticalFailures, check.Name)
			}
		default:
			summary.Unknown++
		}

		summary.ChecksByName[normalizeFingerprintCheckName(check.Name)] = check
	}

	sort.Slice(summary.Checks, func(i, j int) bool {
		return normalizeFingerprintCheckName(summary.Checks[i].Name) < normalizeFingerprintCheckName(summary.Checks[j].Name)
	})
	sort.Strings(summary.FailedChecks)
	sort.Strings(summary.CriticalFailures)

	total := len(checks)
	t.Logf("Sannysoft (%s): %d/%d checks passed (%d failed, %d unknown)", label, summary.Passed, total, summary.Failed, summary.Unknown)
	return summary
}

type sannysoftComparison struct {
	Baseline      string   `json:"baseline"`
	Candidate     string   `json:"candidate"`
	Improved      []string `json:"improved"`
	Regressed     []string `json:"regressed"`
	StillDetected []string `json:"still_detected"`
}

type sannysoftReport struct {
	GeneratedAtUTC string                `json:"generated_at_utc"`
	URL            string                `json:"url"`
	Runs           []sannysoftRunSummary `json:"runs"`
	Comparison     sannysoftComparison   `json:"comparison"`
}

func writeSannysoftReport(path string, report sannysoftReport) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create artifact directory: %w", err)
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	data = append(data, '\n')

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write report file %s: %w", path, err)
	}
	return nil
}

func saveSannysoftScreenshot(t *testing.T, page *rod.Page, path, label string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Logf("WARN: create screenshot directory (%s): %v", label, err)
		return
	}

	bytes, err := page.Screenshot(true, nil)
	if err != nil {
		t.Logf("WARN: capture screenshot (%s): %v", label, err)
		return
	}
	if err := os.WriteFile(path, bytes, 0o644); err != nil {
		t.Logf("WARN: write screenshot (%s): %v", label, err)
		return
	}

	absPath, err := filepath.Abs(path)
	if err == nil {
		t.Logf("Sannysoft screenshot artifact (%s): %s", label, absPath)
		return
	}
	t.Logf("Sannysoft screenshot artifact (%s): core/%s", label, filepath.ToSlash(path))
}

func stealthModeLabel(useStealth bool) string {
	if useStealth {
		return "stealth-on"
	}
	return "stealth-off"
}

func compareSannysoftRuns(baseline, candidate sannysoftRunSummary) (improved, regressed, stillDetected []string) {
	names := make(map[string]struct{}, len(baseline.ChecksByName)+len(candidate.ChecksByName))
	for key := range baseline.ChecksByName {
		names[key] = struct{}{}
	}
	for key := range candidate.ChecksByName {
		names[key] = struct{}{}
	}

	for key := range names {
		before, hasBefore := baseline.ChecksByName[key]
		after, hasAfter := candidate.ChecksByName[key]
		if !hasBefore || !hasAfter {
			continue
		}

		switch {
		case before.Status == "fail" && after.Status == "pass":
			improved = append(improved, after.Name)
		case before.Status == "pass" && after.Status == "fail":
			regressed = append(regressed, after.Name)
		case before.Status == "fail" && after.Status == "fail":
			stillDetected = append(stillDetected, after.Name)
		}
	}

	sort.Strings(improved)
	sort.Strings(regressed)
	sort.Strings(stillDetected)
	return improved, regressed, stillDetected
}

func normalizeFingerprintCheckName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

func waitForSannysoftResults(page *rod.Page, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		hasRows, _, err := page.Has("table tr")
		if err != nil {
			return fmt.Errorf("table probe failed: %w", err)
		}

		if hasRows {
			checks, err := extractSannysoftResults(page)
			if err == nil && len(checks) >= 5 {
				return nil
			}
		}

		time.Sleep(250 * time.Millisecond)
	}

	return fmt.Errorf("results table not ready after %s", timeout)
}

func extractSannysoftResults(page *rod.Page) ([]sannysoftCheck, error) {
	res, err := page.Eval(`() => {
		const parseRGB = (value) => {
			const match = (value || "").match(/rgba?\((\d+),\s*(\d+),\s*(\d+)/i);
			if (!match) return null;
			return [parseInt(match[1], 10), parseInt(match[2], 10), parseInt(match[3], 10)];
		};

		const classify = (text, className, bgColor) => {
			const normalizedText = (text || "").toLowerCase();
			const normalizedClass = (className || "").toLowerCase();
			const rgb = parseRGB(bgColor);

			if (/\b(fail(?:ed)?|detected|bot)\b/.test(normalizedText)) return "fail";
			if (/\b(pass(?:ed)?|ok|success)\b/.test(normalizedText)) return "pass";
			if (/\b(fail(?:ed)?|error|danger|bad|red)\b/.test(normalizedClass)) return "fail";
			if (/\b(pass(?:ed)?|success|ok|good|green)\b/.test(normalizedClass)) return "pass";

			if (rgb) {
				const [r, g, b] = rgb;
				if (r > g + 35 && r > b + 35) return "fail";
				if (g > r + 20 && g > b + 20) return "pass";
			}

			return "unknown";
		};

		const rows = Array.from(document.querySelectorAll("table tr"));
		const seen = new Set();
		const checks = [];

		for (const row of rows) {
			const cells = Array.from(row.querySelectorAll("th, td"));
			if (cells.length < 2) continue;

			const nameCell = cells[0];
			const name = (nameCell.innerText || nameCell.textContent || "").replace(/\s+/g, " ").trim();
			if (!name) continue;
			if (/^(test(\s+name)?|property|status|result)$/i.test(name)) continue;

			const resultCells = cells.slice(1);
			const statusCell =
				resultCells.find((cell) => /\b(result|pass|fail|success|ok)\b/i.test(cell.className || "")) ||
				resultCells.find((cell) => {
					const bg = window.getComputedStyle(cell).backgroundColor || "";
					return bg !== "" && bg !== "transparent" && bg !== "rgba(0, 0, 0, 0)";
				}) ||
				resultCells[0];
			if (!statusCell) continue;

			const statusText = (statusCell.innerText || statusCell.textContent || "").replace(/\s+/g, " ").trim();
			const className = (row.className || "") + " " + (statusCell.className || "");
			const bgColor = window.getComputedStyle(statusCell).backgroundColor || "";
			const dedupeKey = name.toLowerCase();
			if (seen.has(dedupeKey)) continue;
			seen.add(dedupeKey);

			checks.push({
				name,
				status: classify(statusText, className, bgColor),
			});
		}

		return checks;
	}`)
	if err != nil {
		return nil, err
	}

	var checks []sannysoftCheck
	if err := res.Value.Unmarshal(&checks); err != nil {
		return nil, fmt.Errorf("decode sannysoft results: %w", err)
	}
	return checks, nil
}

func isCriticalFingerprintFailure(checkName string) bool {
	name := strings.ToLower(strings.TrimSpace(checkName))
	return slices.ContainsFunc(criticalSannysoftChecks, func(critical string) bool {
		return strings.Contains(name, critical)
	})
}

func closeTestBrowser(t *testing.T, browser *Browser) {
	t.Helper()
	if browser == nil {
		return
	}
	if err := browser.Close(); err != nil {
		t.Logf("close browser: %v", err)
	}
}
