package core

import (
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	extractpkg "github.com/karust/openserp/extract"
)

func TestExtractStructureOptIn(t *testing.T) {
	for _, tc := range []struct {
		query      string
		body, want bool
	}{
		{"", false, false}, {"", true, true},
		{"?preserve_structure=true", false, true},
		{"?preserve_structure=false", true, false},
	} {
		t.Run(tc.query, func(t *testing.T) {
			app := fiber.New()
			s := &Server{}
			app.Get("/", func(c *fiber.Ctx) error {
				req, err := s.baseExtractRequest(c, extractPayload{PreserveStructure: tc.body}, extractpkg.DefaultConfig())
				if err != nil {
					t.Error(err)
					return c.SendStatus(500)
				}
				if req.PreserveStructure != tc.want {
					t.Errorf("preserve_structure=%v, want %v", req.PreserveStructure, tc.want)
				}
				return c.SendStatus(204)
			})
			resp, err := app.Test(httptest.NewRequest("GET", "/"+tc.query, nil))
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
		})
	}
}
