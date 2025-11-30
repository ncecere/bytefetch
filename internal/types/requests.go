package types

type ScrapeRequest struct {
	URL          string `json:"url"`
	OutputFormat string `json:"output_format"`
	UseJS        bool   `json:"use_js"`
}

type BatchScrapeRequest struct {
	URLs         []string `json:"urls"`
	OutputFormat string   `json:"output_format"`
	UseJS        bool     `json:"use_js"`
}

type CrawlRequest struct {
	URL             string `json:"url"`
	MaxDepth        int    `json:"max_depth"`
	SameDomain      bool   `json:"same_domain"`
	OutputFormat    string `json:"output_format"`
	UseJS           bool   `json:"use_js"`
	IncludeSitemaps bool   `json:"include_sitemaps"`
}

type MapRequest struct {
	URL             string `json:"url"`
	MaxDepth        int    `json:"max_depth"`
	SameDomain      bool   `json:"same_domain"`
	IncludeSitemaps bool   `json:"include_sitemaps"`
}
