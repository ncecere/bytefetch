package extract

import (
	"bytes"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type Extracted struct {
	URL   string
	Title string
	Text  string
	HTML  string
	Meta  map[string]string
	Links []string
}

// Extract parses HTML, removes noise, and returns structured content.
func Extract(rawURL string, body []byte) (*Extracted, error) {
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// Drop noisy elements.
	doc.Find("script,style,nav,footer").Remove()

	title := strings.TrimSpace(doc.Find("title").First().Text())
	meta := map[string]string{
		"site_name":   "",
		"title":       title,
		"description": "",
		"language":    "",
		"created_at":  time.Now().UTC().Format(time.RFC3339),
	}

	// Titles and descriptions from standard and Open Graph tags.
	if desc, ok := doc.Find(`meta[name="description"]`).Attr("content"); ok {
		meta["description"] = strings.TrimSpace(desc)
	}
	if meta["description"] == "" {
		if ogDesc, ok := doc.Find(`meta[property="og:description"]`).Attr("content"); ok {
			meta["description"] = strings.TrimSpace(ogDesc)
		}
	}
	if robots, ok := doc.Find(`meta[name="robots"]`).Attr("content"); ok {
		meta["robots"] = strings.ToLower(strings.TrimSpace(robots))
	}
	if ogSite, ok := doc.Find(`meta[property="og:site_name"]`).Attr("content"); ok {
		meta["site_name"] = strings.TrimSpace(ogSite)
	}
	if lang, ok := doc.Find("html").Attr("lang"); ok {
		meta["language"] = strings.TrimSpace(lang)
	}
	if title == "" {
		if ogTitle, ok := doc.Find(`meta[property="og:title"]`).Attr("content"); ok {
			title = strings.TrimSpace(ogTitle)
			meta["title"] = title
		}
	}

	links := make([]string, 0)
	doc.Find("a[href]").Each(func(_ int, s *goquery.Selection) {
		if href, ok := s.Attr("href"); ok {
			if rel, ok := s.Attr("rel"); ok && strings.Contains(strings.ToLower(rel), "nofollow") {
				return
			}
			links = append(links, strings.TrimSpace(href))
		}
	})

	text := strings.Join(strings.Fields(doc.Find("body").Text()), " ")
	html, _ := doc.Find("body").Html()

	return &Extracted{
		URL:   rawURL,
		Title: title,
		Text:  strings.TrimSpace(text),
		HTML:  strings.TrimSpace(html),
		Meta:  meta,
		Links: links,
	}, nil
}
