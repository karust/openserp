package extract

import (
	"context"
	"encoding/json"
	"time"
)

type Mode string

const (
	ModeAuto     Mode = "auto"
	ModeFast     Mode = "fast"
	ModeRendered Mode = "rendered"
)

type ExtractRequest struct {
	URL      string
	Mode     Mode
	ProxyURL string
	LangCode string
	Timeout  time.Duration
	MaxBytes int
	// FullPage selects whole-readable-body extraction instead of the default
	// article-only (trafilatura) extraction. LLM agents fetching arbitrary URLs
	// often want the full page; FullPage keeps nav/feature/landing content that
	// trafilatura strips. The zero value (false) preserves the cleaned default.
	FullPage bool
	// UseLLMSTxt, when set and the URL is a site root, probes /llms-full.txt then
	// /llms.txt and returns that LLM-optimized markdown instead of scraping HTML.
	UseLLMSTxt bool
	// MinRunes is the per-request auto-mode escalation floor: raw output below
	// this many extracted-text runes escalates to a render. 0 uses defaultMinRunes.
	MinRunes int
}

type ExtractResult struct {
	URL         string            `json:"url"`
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Markdown    string            `json:"markdown"`
	Text        string            `json:"text"`
	Headings    []Heading         `json:"headings,omitempty"`
	Links       []Link            `json:"links,omitempty"`
	Canonical   string            `json:"canonical,omitempty"`
	Lang        string            `json:"lang,omitempty"`
	SchemaOrg   []json.RawMessage `json:"schema_org,omitempty"`
	OGTags      map[string]string `json:"og_tags,omitempty"`
	Meta        ExtractMeta       `json:"meta"`
}

type Heading struct {
	Level int    `json:"level"`
	Text  string `json:"text"`
}

type Link struct {
	Text string `json:"text"`
	URL  string `json:"url"`
}

type ExtractMeta struct {
	ModeUsed  string `json:"mode_used"`
	FetchedAt string `json:"fetched_at"`
	Bytes     int    `json:"bytes"`
	TookMs    int64  `json:"took_ms"`
}

type FetchResponse struct {
	StatusCode int
	Body       []byte
}

type RawFetcher func(ctx context.Context, req ExtractRequest) (*FetchResponse, error)

type RenderedFetcher func(ctx context.Context, req ExtractRequest) (*FetchResponse, error)
