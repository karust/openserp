package google

import (
	"testing"

	"github.com/karust/openserp/core"
)

func TestDateWindowValidation(t *testing.T) {
	for _, date := range []string{"20261301..20261310", "20260230..20260301", "20260102..20260101"} {
		q := core.Query{Text: "test", DateInterval: date}
		if _, err := BuildURL(q); err == nil {
			t.Errorf("accepted %s", date)
		}
		if _, err := BuildImageURL(q); err == nil {
			t.Errorf("accepted image %s", date)
		}
	}
}
