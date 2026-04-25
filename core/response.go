package core

import "time"

// QueryEcho echoes the interpreted query parameters back to the client.
type QueryEcho struct {
	Text             string   `json:"text"`
	Lang             string   `json:"lang,omitempty"`
	EnginesRequested []string `json:"engines_requested"`
}

// ResponseMeta carries request-level metadata for observability and debugging.
type ResponseMeta struct {
	RequestID     string   `json:"request_id"`
	RequestedAt   string   `json:"requested_at"`
	TookMs        int64    `json:"took_ms"`
	EnginesFailed []string `json:"engines_failed"`
	Version       string   `json:"version"`
}

// Pagination carries cursor information for client-side loop termination.
type Pagination struct {
	Page      int  `json:"page"`
	HasMore   bool `json:"has_more"`
	NextStart int  `json:"next_start"`
}

// Envelope is the top-level v1 response wrapper for all search endpoints.
type Envelope struct {
	Query      QueryEcho    `json:"query"`
	Meta       ResponseMeta `json:"meta"`
	Results    []Result     `json:"results"`
	Pagination Pagination   `json:"pagination"`
	// Clusters is only populated by /mega/search (see clusters.go).
	Clusters *[]Cluster `json:"clusters,omitempty"`
}

// Cluster groups results that refer to the same canonical URL across engines.
// Populated only by /mega/search. Full type defined in clusters.go.
type Cluster struct {
	ID           string              `json:"id"`
	CanonicalURL string              `json:"canonical_url"`
	Domain       string              `json:"domain"`
	Title        string              `json:"title"`
	Occurrences  []ClusterOccurrence `json:"occurrences"`
	EnginesCount int                 `json:"engines_count"`
	BestRank     int                 `json:"best_rank"`
	Score        float64             `json:"score"`
}

// ClusterOccurrence links one engine result back into the flat results list.
type ClusterOccurrence struct {
	Engine   string `json:"engine"`
	Rank     int    `json:"rank"`
	ResultID string `json:"result_id"`
}

// ImageEnvelope is the top-level v1 response wrapper for image search endpoints.
type ImageEnvelope struct {
	Query      QueryEcho     `json:"query"`
	Meta       ResponseMeta  `json:"meta"`
	Results    []ImageResult `json:"results"`
	Pagination Pagination    `json:"pagination"`
}

const apiVersion = "1.0"

// NewEnvelope builds a fresh Envelope pre-filled with query echo and an open
// meta block. Call Finalize before serializing.
func NewEnvelope(q Query, requestID string, startedAt time.Time, engines []string) *Envelope {
	return &Envelope{
		Query: QueryEcho{
			Text:             q.Text,
			Lang:             q.LangCode,
			EnginesRequested: engines,
		},
		Meta: ResponseMeta{
			RequestID:     requestID,
			RequestedAt:   startedAt.UTC().Format(time.RFC3339),
			EnginesFailed: []string{},
			Version:       apiVersion,
		},
		Results:    []Result{},
		Pagination: Pagination{},
	}
}

// NewImageEnvelope builds a fresh ImageEnvelope.
func NewImageEnvelope(q Query, requestID string, startedAt time.Time, engines []string) *ImageEnvelope {
	return &ImageEnvelope{
		Query: QueryEcho{
			Text:             q.Text,
			Lang:             q.LangCode,
			EnginesRequested: engines,
		},
		Meta: ResponseMeta{
			RequestID:     requestID,
			RequestedAt:   startedAt.UTC().Format(time.RFC3339),
			EnginesFailed: []string{},
			Version:       apiVersion,
		},
		Results:    []ImageResult{},
		Pagination: Pagination{},
	}
}

// Finalize stamps the elapsed time and computes pagination fields.
func (e *Envelope) Finalize(startedAt time.Time, q Query) {
	e.Meta.TookMs = time.Since(startedAt).Milliseconds()

	limit := q.Limit
	if limit <= 0 {
		limit = 25
	}
	page := q.Start/limit + 1
	e.Pagination = Pagination{
		Page:      page,
		HasMore:   len(e.Results) >= limit,
		NextStart: q.Start + limit,
	}
}

// Finalize stamps the elapsed time and computes pagination fields.
func (e *ImageEnvelope) Finalize(startedAt time.Time, q Query) {
	e.Meta.TookMs = time.Since(startedAt).Milliseconds()

	limit := q.Limit
	if limit <= 0 {
		limit = 25
	}
	page := q.Start/limit + 1
	e.Pagination = Pagination{
		Page:      page,
		HasMore:   len(e.Results) >= limit,
		NextStart: q.Start + limit,
	}
}
