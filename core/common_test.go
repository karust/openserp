package core

import "testing"

func TestShouldFetchResultPage(t *testing.T) {
	tests := []struct {
		name         string
		collected    int
		limit        int
		pagesFetched int
		want         bool
	}{
		{
			name:         "always fetch first page",
			limit:        10,
			pagesFetched: 0,
			want:         true,
		},
		{
			name:         "unset limit stops after first page",
			collected:    8,
			limit:        0,
			pagesFetched: 1,
			want:         false,
		},
		{
			name:         "default limit stops after first page even if short",
			collected:    8,
			limit:        10,
			pagesFetched: 1,
			want:         false,
		},
		{
			name:         "larger limit can fetch another short page",
			collected:    8,
			limit:        11,
			pagesFetched: 1,
			want:         true,
		},
		{
			name:         "larger limit stops when satisfied",
			collected:    11,
			limit:        11,
			pagesFetched: 1,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ShouldFetchResultPage(tt.collected, tt.limit, tt.pagesFetched)
			if got != tt.want {
				t.Fatalf("ShouldFetchResultPage(%d, %d, %d) = %t, want %t",
					tt.collected, tt.limit, tt.pagesFetched, got, tt.want)
			}
		})
	}
}
