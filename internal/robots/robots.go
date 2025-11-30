package robots

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/temoto/robotstxt"
)

type Client struct {
	httpClient *http.Client
	cache      map[string]*robotstxt.RobotsData
	lastFetch  map[string]time.Time
	mu         sync.Mutex
	failOpen   bool
}

func New(timeout time.Duration, failOpen bool) *Client {
	return &Client{
		httpClient: &http.Client{Timeout: timeout},
		cache:      make(map[string]*robotstxt.RobotsData),
		lastFetch:  make(map[string]time.Time),
		failOpen:   failOpen,
	}
}

// Allowed returns whether the path is allowed for the given user-agent on host.
func (c *Client) Allowed(rawURL, userAgent string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return true // invalid URL, fail-open
	}
	host := strings.ToLower(u.Host)

	data := c.getData(host, u.Scheme)

	if data == nil {
		return c.failOpen
	}
	ua := userAgent
	if ua == "" {
		ua = "Mozilla/5.0"
	}
	return data.TestAgent(rawURL, ua)
}

// Delay returns the crawl delay for a host/user-agent, capped by defaultDelay (defaultDelay acts as minimum).
func (c *Client) Delay(rawURL, userAgent string, defaultDelay time.Duration) time.Duration {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return defaultDelay
	}
	host := strings.ToLower(u.Host)
	data := c.getData(host, u.Scheme)
	if data == nil {
		return defaultDelay
	}
	ua := userAgent
	if ua == "" {
		ua = "Mozilla/5.0"
	}
	g := data.FindGroup(ua)
	if g != nil && g.CrawlDelay > 0 && g.CrawlDelay > defaultDelay {
		return g.CrawlDelay
	}
	return defaultDelay
}

// Wait respects crawl delay per host, using robots.txt or default delay. Returns on context cancel.
func (c *Client) Wait(ctx context.Context, rawURL, userAgent string, defaultDelay time.Duration) error {
	delay := c.Delay(rawURL, userAgent, defaultDelay)
	if delay <= 0 {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil
	}
	host := strings.ToLower(u.Host)

	c.mu.Lock()
	last := c.lastFetch[host]
	target := time.Now()
	if !last.IsZero() {
		if next := last.Add(delay); next.After(target) {
			target = next
		}
	}
	c.mu.Unlock()

	sleep := time.Until(target)
	if sleep > 0 {
		timer := time.NewTimer(sleep)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
		}
	}

	c.mu.Lock()
	c.lastFetch[host] = time.Now()
	c.mu.Unlock()
	return nil
}

// Sitemaps returns sitemap URLs advertised by robots.txt for the given URL.
func (c *Client) Sitemaps(rawURL string) []string {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" {
		return nil
	}
	data := c.getData(strings.ToLower(u.Host), u.Scheme)
	if data == nil || len(data.Sitemaps) == 0 {
		return nil
	}
	out := make([]string, 0, len(data.Sitemaps))
	for _, sm := range data.Sitemaps {
		out = append(out, sm)
	}
	return out
}

func (c *Client) getData(host, scheme string) *robotstxt.RobotsData {
	c.mu.Lock()
	data, ok := c.cache[host]
	c.mu.Unlock()
	if ok {
		return data
	}

	data = c.fetch(host, scheme)
	c.mu.Lock()
	c.cache[host] = data
	c.mu.Unlock()
	return data
}

func (c *Client) fetch(host, scheme string) *robotstxt.RobotsData {
	if scheme == "" {
		scheme = "https"
	}
	url := scheme + "://" + host + "/robots.txt"
	resp, err := c.httpClient.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	data, err := robotstxt.FromResponse(resp)
	if err != nil {
		return nil
	}
	return data
}
