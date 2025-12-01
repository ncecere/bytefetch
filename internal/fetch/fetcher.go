package fetch

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/ncecere/bytefetch/internal/config"
	"github.com/ncecere/bytefetch/internal/metrics"
)

type Result struct {
	Body     []byte
	FinalURL string
}

// Fetcher performs HTTP fetches. JS rendering via Rod can be added later.
type Fetcher struct {
	client     *http.Client
	userAgent  string
	maxBytes   int64
	renderer   *Renderer
	browserCfg config.BrowserConfig
}

func NewFetcher(httpCfg config.HTTPConfig, renderer *Renderer, browserCfg config.BrowserConfig) *Fetcher {
	client := &http.Client{
		Timeout: httpCfg.Timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= httpCfg.MaxRedirects {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	return &Fetcher{
		client:     client,
		userAgent:  httpCfg.UserAgent,
		maxBytes:   httpCfg.MaxBodyBytes.Int64(),
		renderer:   renderer,
		browserCfg: browserCfg,
	}
}

func (f *Fetcher) Fetch(ctx context.Context, rawURL string, useJS bool) (*Result, error) {
	if useJS && f.renderer != nil && f.browserCfg.Enabled {
		start := time.Now()
		res, err := f.renderer.Render(ctx, rawURL)
		if err == nil {
			metrics.RenderDuration.WithLabelValues(hostFromURL(res.FinalURL)).Observe(time.Since(start).Seconds())
		}
		return res, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	if f.userAgent != "" {
		req.Header.Set("User-Agent", f.userAgent)
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	host := hostFromURL(resp.Request.URL.String())
	metrics.FetchHTTPStatus.WithLabelValues(host, strconv.Itoa(resp.StatusCode)).Inc()

	reader := io.LimitReader(resp.Body, f.maxBytes)
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	finalURL := resp.Request.URL.String()

	return &Result{
		Body:     body,
		FinalURL: finalURL,
	}, nil
}

func hostFromURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// Ready checks browser availability by rendering about:blank when renderer is enabled.
func (f *Fetcher) Ready(ctx context.Context) error {
	if f.renderer == nil || !f.browserCfg.Enabled {
		return nil
	}
	_, err := f.renderer.Render(ctx, "about:blank")
	return err
}

// WithTimeout returns a derived context with the fetch timeout.
func WithTimeout(ctx context.Context, d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, d)
}
