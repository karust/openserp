package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestClassifyChallengeDocumentText(t *testing.T) {
	for _, tc := range []struct {
		name string
		html string
		want error
	}{
		{"script and style markers", "<script>verify you are human</script><style>/* no results found */</style><p>Results</p>", nil},
		{"captcha across blocks", "<p>Verify you are</p><p>human</p>", ErrCaptcha},
		{"empty across a line break", "<p>No results<br>found</p>", ErrEmptyResult},
		{"captcha takes priority", "<p>No results found</p><p>Verify you are human</p>", ErrCaptcha},
	} {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tc.html))
			if err != nil {
				t.Fatal(err)
			}
			got := ClassifyChallengeDocument(doc, DocSignals{
				CaptchaMarkers: []string{"verify you are human"},
				EmptyMarkers:   []string{"no results found"},
			})
			if !errors.Is(got, tc.want) {
				t.Fatalf("classification = %v, want %v", got, tc.want)
			}
		})
	}
}
