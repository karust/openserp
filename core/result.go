package core

// ResultType is the SERP block type for a search result.
type ResultType string

const (
	ResultTypeOrganic          ResultType = "organic"
	ResultTypeAd               ResultType = "ad"
	ResultTypeFeaturedSnippet  ResultType = "featured_snippet"
	ResultTypeKnowledgePanel   ResultType = "knowledge_panel"
	ResultTypePeopleAlsoAsk    ResultType = "people_also_ask"
	ResultTypeVideo            ResultType = "video"
	ResultTypeImage            ResultType = "image"
	ResultTypeNews             ResultType = "news"
	ResultTypeShopping         ResultType = "shopping"
	ResultTypeLocal            ResultType = "local"
	ResultTypeAnswerBox        ResultType = "answer_box"
	ResultTypeAISummary        ResultType = "ai_summary"
	ResultTypeRelatedQuestions ResultType = "related_questions"
	ResultTypeRelatedSearches  ResultType = "related_searches"
	ResultTypeSitelinks        ResultType = "sitelinks"
	ResultTypeVideos           ResultType = "videos"
	ResultTypeImagesInline     ResultType = "images_inline"
	ResultTypeCalculator       ResultType = "calculator"
	ResultTypeWeather          ResultType = "weather"
	ResultTypeDictionary       ResultType = "dictionary"
)

// Position describes where a result sits in the overall result stream.
type Position struct {
	// Absolute is the 1-based rank counting from the first result of the first page,
	// across both organic and ad blocks. Always emitted so SEO callers can plot
	// rank vs. on-page position without inferring it from the result order.
	Absolute int `json:"absolute"`
}

// DomainInfo carries TLD-derived category signals for a result domain.
type DomainInfo struct {
	TLD string `json:"tld,omitempty"`
	SLD string `json:"sld,omitempty"`
	// Category is one of "gov", "edu", "mil", "news", "forum", "marketplace",
	// "social", or "" when the domain does not match any known category.
	Category string `json:"category"`
}

// Classification holds URL-path heuristic hints for downstream consumers.
type Classification struct {
	ContentType string `json:"content_type,omitempty"`
	SourceHint  string `json:"source_hint,omitempty"`
}

// ExtractedContent carries one enriched target page. Content holds a single
// representation chosen by the response format (markdown for json/ndjson/markdown,
// plain text for text), with Format naming which it is — no duplicated blobs.
type ExtractedContent struct {
	Title     string `json:"title,omitempty"`
	Format    string `json:"format,omitempty"`
	Content   string `json:"content,omitempty"`
	ModeUsed  string `json:"mode_used,omitempty"`
	FetchedAt string `json:"fetched_at,omitempty"`
	Error     string `json:"error,omitempty"`
}

// FeatureItem is one child entry inside a grouped SERP feature.
type FeatureItem struct {
	Title string `json:"title,omitempty"`
	Text  string `json:"text,omitempty"`
	Link  string `json:"link,omitempty"`
}

// FeatureLink is a source or citation associated with a SERP feature.
type FeatureLink struct {
	Title string `json:"title,omitempty"`
	URL   string `json:"url,omitempty"`
}

// SerpFeature is a normalized non-organic SERP module surfaced separately
// from rankable results.
type SerpFeature struct {
	ID              string        `json:"id"`
	Engine          string        `json:"engine"`
	Type            ResultType    `json:"type"`
	Title           string        `json:"title,omitempty"`
	Text            string        `json:"text,omitempty"`
	Items           []FeatureItem `json:"items,omitempty"`
	Links           []FeatureLink `json:"links,omitempty"`
	SourceResultIDs []string      `json:"source_result_ids,omitempty"`
	Position        *Position     `json:"position,omitempty"`
	Confidence      float64       `json:"confidence,omitempty"`
	ExtractedAt     string        `json:"extracted_at"`
}

// Result is the v2 normalized result returned in search responses. Optional
// fields (Position, DomainInfo, Classification) are omitted when empty.
type Result struct {
	ID             string            `json:"id"`
	Rank           int               `json:"rank"`
	Type           ResultType        `json:"type"`
	Title          string            `json:"title"`
	URL            string            `json:"url"`
	DisplayURL     string            `json:"display_url"`
	Snippet        string            `json:"snippet"`
	Domain         string            `json:"domain"`
	Favicon        string            `json:"favicon"`
	Position       *Position         `json:"position,omitempty"`
	Engine         string            `json:"engine"`
	DomainInfo     *DomainInfo       `json:"domain_info,omitempty"`
	Classification *Classification   `json:"classification,omitempty"`
	Extracted      *ExtractedContent `json:"extracted,omitempty"`
}

// ImageData holds image-specific URL and dimension fields.
type ImageData struct {
	URL       string `json:"url"`
	Thumbnail string `json:"thumbnail,omitempty"`
	Width     int    `json:"width,omitempty"`
	Height    int    `json:"height,omitempty"`
}

// ImageSource holds page-level context for an image result.
type ImageSource struct {
	PageURL string `json:"page_url"`
	Domain  string `json:"domain"`
}

// ImageResult is the v2 shape for image search results.
type ImageResult struct {
	ID     string      `json:"id"`
	Rank   int         `json:"rank"`
	Type   ResultType  `json:"type"`
	Title  string      `json:"title"`
	Image  ImageData   `json:"image"`
	Source ImageSource `json:"source"`
	Engine string      `json:"engine"`
}
