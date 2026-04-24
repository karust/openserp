package core

// ResultType is the SERP block type for a search result.
type ResultType string

const (
	ResultTypeOrganic        ResultType = "organic"
	ResultTypeAd             ResultType = "ad"
	ResultTypeFeaturedSnippet ResultType = "featured_snippet"
	ResultTypeKnowledgePanel ResultType = "knowledge_panel"
	ResultTypePeopleAlsoAsk  ResultType = "people_also_ask"
	ResultTypeVideo          ResultType = "video"
	ResultTypeImage          ResultType = "image"
	ResultTypeNews           ResultType = "news"
	ResultTypeShopping       ResultType = "shopping"
	ResultTypeLocal          ResultType = "local"
	ResultTypeAnswerBox      ResultType = "answer_box"
)

// Position describes where a result sits in the overall result stream.
type Position struct {
	// Absolute is the 1-based rank counting from the first result of the first page.
	Absolute int `json:"absolute"`
	// Page is the 1-based page number derived from start/limit.
	Page int `json:"page"`
	// OnPage is the 1-based rank within this page.
	OnPage int `json:"on_page"`
}

// RichData is a placeholder for future structured SERP features such as star
// ratings, prices, or sitelinks. It is null in v1.0.
type RichData struct {
	Stars    *float64          `json:"stars,omitempty"`
	Reviews  *int              `json:"reviews,omitempty"`
	Price    *string           `json:"price,omitempty"`
	Sitelinks []RichSitelink   `json:"sitelinks,omitempty"`
}

// RichSitelink is one navigational sub-link shown below a result.
type RichSitelink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// DomainInfo carries TLD-derived category signals for a result domain.
type DomainInfo struct {
	TLD           string `json:"tld"`
	SLD           string `json:"sld"`
	IsGov         bool   `json:"is_gov"`
	IsEdu         bool   `json:"is_edu"`
	IsMil         bool   `json:"is_mil"`
	IsNews        bool   `json:"is_news"`
	IsForum       bool   `json:"is_forum"`
	IsMarketplace bool   `json:"is_marketplace"`
	IsSocial      bool   `json:"is_social"`
}

// Classification holds URL-path heuristic hints for downstream consumers.
type Classification struct {
	ContentType string `json:"content_type"`
	SourceHint  string `json:"source_hint"`
}

// Result is the v1 normalized result returned in every search response.
type Result struct {
	ID           string         `json:"id"`
	Rank         int            `json:"rank"`
	Type         ResultType     `json:"type"`
	Title        string         `json:"title"`
	URL          string         `json:"url"`
	DisplayURL   string         `json:"display_url"`
	Snippet      string         `json:"snippet"`
	Domain       string         `json:"domain"`
	Favicon      string         `json:"favicon"`
	IsAd         bool           `json:"is_ad"`
	Position     Position       `json:"position"`
	Engine       string         `json:"engine"`
	Rich         *RichData      `json:"rich"`
	EngineMeta   map[string]any `json:"engine_meta"`
	DomainInfo   *DomainInfo    `json:"domain_info,omitempty"`
	Classification *Classification `json:"classification,omitempty"`
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

// ImageResult is the v1 shape for image search results.
type ImageResult struct {
	ID         string         `json:"id"`
	Rank       int            `json:"rank"`
	Type       ResultType     `json:"type"`
	Title      string         `json:"title"`
	Image      ImageData      `json:"image"`
	Source     ImageSource    `json:"source"`
	Engine     string         `json:"engine"`
	EngineMeta map[string]any `json:"engine_meta"`
}
