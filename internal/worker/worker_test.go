package worker

import (
	"context"
	"testing"
	"time"

	"github.com/ncecere/bytefetch/internal/config"
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

func TestBuildLinkTasksDedupeAndLimit(t *testing.T) {
	planner := &fakePlanner{
		seen:  map[string]bool{"https://example.com/about": true},
		count: 1,
	}
	cfg := &config.Config{
		Crawl: config.CrawlConfig{
			DedupeCache: true,
		},
		JobDefaults: config.JobDefaults{
			MaxTasks: 2,
		},
	}
	links := []string{
		"https://example.com/",
		"https://example.com/about",
		"https://example.com/docs",
	}
	tasks, err := buildLinkTasks(context.Background(), planner, "job", links, 1, cfg)
	if err != nil {
		t.Fatalf("buildLinkTasks returned error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task after dedupe/limit, got %d (%v)", len(tasks), tasks)
	}
	if tasks[0].RequestedURL != "https://example.com/" {
		t.Fatalf("unexpected task URL %s", tasks[0].RequestedURL)
	}
}

type fakePlanner struct {
	seen  map[string]bool
	count int
}

func (f *fakePlanner) HasSeenCanonical(ctx context.Context, url string) (bool, error) {
	if f.seen == nil {
		return false, nil
	}
	return f.seen[url], nil
}

func (f *fakePlanner) TaskCount(ctx context.Context, jobID string) (int, error) {
	return f.count, nil
}
