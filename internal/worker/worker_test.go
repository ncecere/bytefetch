package worker

import (
	"testing"
	"time"

	"github.com/nickcecere/bullnose/internal/config"
)

func TestFilterLinksSameDomainAndBlocked(t *testing.T) {
	cfg := &config.Config{
		Crawl: config.CrawlConfig{
			StripQuery:      true,
			BlockExtensions: []string{".pdf"},
			AllowedSchemes:  []string{"http", "https"},
		},
	}
	base := "https://example.com/root"
	links := []string{
		"https://example.com/about?utm=test",
		"https://example.com/file.pdf",
		"/contact#section",
		"https://other.com/",
	}

	got := filterLinks(base, links, true, cfg)
	want := []string{
		"https://example.com/about",
		"https://example.com/contact",
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d links, got %d (%v)", len(want), len(got), got)
	}
	for i, v := range want {
		if got[i] != v {
			t.Fatalf("want %s got %s", v, got[i])
		}
	}
}

func TestBackoffRespectsMax(t *testing.T) {
	base := 100 * time.Millisecond
	max := 2 * time.Second
	for i := 1; i < 10; i++ {
		if d := backoff(base, max, i); d > max {
			t.Fatalf("backoff exceeded max: %s", d)
		}
	}
}
