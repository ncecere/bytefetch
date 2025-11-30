package server

import (
	"net/url"
	"strings"

	"github.com/nickcecere/bullnose/internal/format"
	"github.com/nickcecere/bullnose/internal/types"
)

func validateScrapeRequest(req *types.ScrapeRequest) error {
	if req.URL == "" || !validURL(req.URL) {
		return errBadURL
	}
	if !validFormat(req.OutputFormat) {
		return errBadFormat
	}
	return nil
}

func validateBatchRequest(req *types.BatchScrapeRequest) error {
	if len(req.URLs) == 0 {
		return errNoURLs
	}
	for _, u := range req.URLs {
		if !validURL(u) {
			return errBadURL
		}
	}
	if !validFormat(req.OutputFormat) {
		return errBadFormat
	}
	return nil
}

func validateCrawlRequest(req *types.CrawlRequest) error {
	if req.URL == "" || !validURL(req.URL) {
		return errBadURL
	}
	if req.MaxDepth < 0 {
		return errBadDepth
	}
	if !validFormat(req.OutputFormat) {
		return errBadFormat
	}
	return nil
}

func validateMapRequest(req *types.MapRequest) error {
	if req.URL == "" || !validURL(req.URL) {
		return errBadURL
	}
	if req.MaxDepth < 0 {
		return errBadDepth
	}
	return nil
}

var (
	errBadURL    = errorString("invalid url")
	errBadFormat = errorString("invalid output_format")
	errBadDepth  = errorString("invalid max_depth")
	errNoURLs    = errorString("no urls provided")
)

type errorString string

func (e errorString) Error() string { return string(e) }

func validURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Host == "" {
		return false
	}
	return true
}

func validFormat(f string) bool {
	switch strings.ToLower(f) {
	case format.FormatMarkdown, format.FormatHTML, format.FormatText:
		return true
	default:
		return false
	}
}
