package core

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gofiber/fiber/v2"
)

// TestInitFromContextExtractParams verifies how the unified extract knob and its
// tuning params map onto Query.Extract / Query.ExtractTop. Key behaviors:
//   - extract is bool-or-int: extract=0/false off, extract=true/1 → top 1,
//     extract=N → top N (clamped to [1,5]).
//   - extract_mode/min_runes imply extraction (top defaults to 1), but an
//     explicit extract=0 still wins over them.
func TestInitFromContextExtractParams(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		query       string
		wantExtract bool
		wantTop     int
	}{
		{"no params defaults off", "?text=q", false, 1},
		{"extract=true means top 1", "?text=q&extract=true", true, 1},
		{"extract=1 means top 1", "?text=q&extract=1", true, 1},
		{"extract=3 means top 3", "?text=q&extract=3", true, 3},
		{"extract=N clamps high", "?text=q&extract=99", true, 5},
		{"extract=0 disables", "?text=q&extract=0", false, 1},
		{"extract=false disables", "?text=q&extract=false", false, 1},
		{"extract_mode implies extract", "?text=q&extract_mode=fast", true, 1},
		{"min_runes implies extract", "?text=q&min_runes=200", true, 1},
		{"explicit extract=0 overrides tuning", "?text=q&extract=0&extract_mode=fast", false, 1},
	}

	app := fiber.New()
	app.Get("/probe", func(c *fiber.Ctx) error {
		q := Query{}
		if err := q.InitFromContext(c); err != nil {
			return c.Status(http.StatusBadRequest).SendString(err.Error())
		}
		extract := "0"
		if q.Extract {
			extract = "1"
		}
		c.Set("X-Extract", extract)
		c.Set("X-Extract-Top", strconv.Itoa(q.ExtractTop))
		return c.SendStatus(http.StatusOK)
	})

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/probe"+tt.query, nil)
			resp, err := app.Test(req, -1)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("unexpected status %d for %s", resp.StatusCode, tt.query)
			}
			gotExtract := resp.Header.Get("X-Extract") == "1"
			if gotExtract != tt.wantExtract {
				t.Errorf("%s: Extract = %v, want %v", tt.query, gotExtract, tt.wantExtract)
			}
			if got := resp.Header.Get("X-Extract-Top"); got != strconv.Itoa(tt.wantTop) {
				t.Errorf("%s: ExtractTop = %s, want %d", tt.query, got, tt.wantTop)
			}
		})
	}
}
