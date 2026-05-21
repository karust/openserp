package core

import "testing"

func TestProfileRegionHint(t *testing.T) {
	tests := []struct {
		name string
		q    Query
		want string
	}{
		{name: "empty", q: Query{}, want: ""},
		{name: "lang only", q: Query{LangCode: "en"}, want: "en"},
		{name: "region country combines with lang", q: Query{LangCode: "en", Region: "DE"}, want: "en-DE"},
		{name: "region locale combines with lang", q: Query{LangCode: "en", Region: "en-GB"}, want: "en-GB"},
		{name: "region country without lang", q: Query{Region: "DE"}, want: "DE"},
		{name: "yandex numeric region falls back to lang", q: Query{LangCode: "ru", Region: "213"}, want: "ru"},
		{name: "yandex numeric region without lang stays empty", q: Query{Region: "213"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := profileRegionHint(tt.q); got != tt.want {
				t.Fatalf("profileRegionHint = %q, want %q", got, tt.want)
			}
		})
	}
}
