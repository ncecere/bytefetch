package types

import (
	"bytes"
	"encoding/json"
	"strings"
)

const OutputsMetaKey = "outputs"

type ScrapeRequest struct {
	URL           string     `json:"url"`
	OutputFormats FormatList `json:"output_format"`
	UseJS         bool       `json:"use_js"`
}

type BatchScrapeRequest struct {
	URLs          []string   `json:"urls"`
	OutputFormats FormatList `json:"output_format"`
	UseJS         bool       `json:"use_js"`
}

type CrawlRequest struct {
	URL             string     `json:"url"`
	MaxDepth        int        `json:"max_depth"`
	SameDomain      bool       `json:"same_domain"`
	OutputFormats   FormatList `json:"output_format"`
	UseJS           bool       `json:"use_js"`
	IncludeSitemaps bool       `json:"include_sitemaps"`
}

type MapRequest struct {
	URL             string `json:"url"`
	MaxDepth        int    `json:"max_depth"`
	SameDomain      bool   `json:"same_domain"`
	IncludeSitemaps bool   `json:"include_sitemaps"`
}

type FormatList []string

func (f *FormatList) UnmarshalJSON(data []byte) error {
	data = bytes.TrimSpace(data)
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		*f = nil
		return nil
	}
	if data[0] == '"' {
		var v string
		if err := json.Unmarshal(data, &v); err != nil {
			return err
		}
		*f = normalizeFormats([]string{v})
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return err
	}
	*f = normalizeFormats(arr)
	return nil
}

func (f FormatList) MarshalJSON() ([]byte, error) {
	return json.Marshal([]string(f))
}

func (f FormatList) Values(defaultFormat string) []string {
	vals := normalizeFormats([]string(f))
	if len(vals) == 0 {
		return []string{strings.ToLower(defaultFormat)}
	}
	return vals
}

func normalizeFormats(in []string) FormatList {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(strings.ToLower(v))
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return FormatList(out)
}

func FormatsFromAny(raw any, defaultFormat string) []string {
	if raw == nil {
		return []string{strings.ToLower(defaultFormat)}
	}
	switch v := raw.(type) {
	case string:
		return FormatList([]string{v}).Values(defaultFormat)
	case []string:
		return FormatList(v).Values(defaultFormat)
	case FormatList:
		return v.Values(defaultFormat)
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return []string{strings.ToLower(defaultFormat)}
	}
	var list FormatList
	if err := json.Unmarshal(buf, &list); err != nil {
		return []string{strings.ToLower(defaultFormat)}
	}
	return list.Values(defaultFormat)
}
