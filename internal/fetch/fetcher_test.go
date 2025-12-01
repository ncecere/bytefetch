package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ncecere/bytefetch/internal/config"
)

func TestFetcherFetchHTTP(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("hello world"))
	}))
	defer server.Close()

	httpCfg := config.HTTPConfig{
		Timeout:      2 * time.Second,
		MaxRedirects: 3,
		MaxBodyBytes: config.ByteSize(1024),
		UserAgent:    "ByteFetchTest/1.0",
	}
	f := NewFetcher(httpCfg, nil, config.BrowserConfig{Enabled: false})

	res, err := f.Fetch(context.Background(), server.URL, false)
	if err != nil {
		t.Fatalf("fetch returned error: %v", err)
	}
	if got := string(res.Body); got != "hello world" {
		t.Fatalf("unexpected body %q", got)
	}
	if res.FinalURL != server.URL {
		t.Fatalf("expected final url %s, got %s", server.URL, res.FinalURL)
	}
}
