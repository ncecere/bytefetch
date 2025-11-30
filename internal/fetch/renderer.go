package fetch

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"
	"github.com/ncecere/bullnose/internal/config"
	"github.com/ncecere/bullnose/internal/metrics"
	"net/url"
)

type Renderer struct {
	cfg      config.BrowserConfig
	browser  *rod.Browser
	mu       sync.Mutex
	pagePool chan *pooledPage
	closed   bool
	hostSem  map[string]chan struct{}
}

// Ready ensures the browser is responsive by opening a temp page.
func (r *Renderer) Ready(ctx context.Context) error {
	if !r.cfg.Enabled || r.browser == nil {
		return nil
	}
	page, err := r.browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		return err
	}
	defer page.Close()
	ctx, cancel := context.WithTimeout(ctx, r.cfg.NavTimeout)
	defer cancel()
	return page.Context(ctx).Navigate("about:blank")
}

type pooledPage struct {
	page     *rod.Page
	acquired time.Time
}

func NewRenderer(cfg config.BrowserConfig) (*Renderer, error) {
	r := &Renderer{cfg: cfg}
	if !cfg.Enabled {
		return r, nil
	}
	l := launcher.New().Headless(cfg.Headless)
	if cfg.ExecutablePath != "" {
		l = l.Bin(cfg.ExecutablePath)
	}
	// Disable sandbox for container friendliness; adjust as needed.
	l = l.Append("--no-sandbox")

	if err := r.launch(l); err != nil {
		return nil, err
	}
	if cfg.PoolSize <= 0 {
		cfg.PoolSize = 2
	}
	if cfg.PerHostLimit <= 0 {
		cfg.PerHostLimit = 2
	}
	r.pagePool = make(chan *pooledPage, cfg.PoolSize)
	r.hostSem = make(map[string]chan struct{})
	for i := 0; i < cfg.PoolSize; i++ {
		p, err := r.browser.Page(proto.TargetCreateTarget{})
		if err != nil {
			return nil, err
		}
		r.pagePool <- &pooledPage{page: p, acquired: time.Now()}
	}
	metrics.BrowserPoolAvailable.Set(float64(len(r.pagePool)))
	if cfg.MaxPageLifetime > 0 {
		go r.healthLoop()
	}
	return r, nil
}

func (r *Renderer) Close() {
	if r.browser != nil {
		_ = r.browser.Close()
	}
	r.mu.Lock()
	r.closed = true
	close(r.pagePool)
	r.mu.Unlock()
}

func (r *Renderer) Render(ctx context.Context, rawURL string) (*Result, error) {
	if !r.cfg.Enabled {
		return nil, fmt.Errorf("renderer disabled")
	}
	releaseHost, err := r.acquireHost(ctx, rawURL)
	if err != nil {
		metrics.BrowserRenderFailures.WithLabelValues(hostFromURL(rawURL), "acquire_host").Inc()
		return nil, err
	}
	defer releaseHost()

	var p *pooledPage
	select {
	case p = <-r.pagePool:
	default:
		page, err := r.newPage()
		if err != nil {
			metrics.BrowserRenderFailures.WithLabelValues(hostFromURL(rawURL), "new_page").Inc()
			return nil, err
		}
		p = &pooledPage{page: page, acquired: time.Now()}
	}
	metrics.BrowserPoolInUse.Inc()
	metrics.BrowserPoolAvailable.Set(float64(len(r.pagePool)))
	defer func() {
		r.recyclePage(p)
		metrics.BrowserPoolAvailable.Set(float64(len(r.pagePool)))
		metrics.BrowserPoolInUse.Dec()
	}()

	pctx := p.page.Timeout(r.cfg.NavTimeout)
	if err := pctx.Navigate(rawURL); err != nil {
		metrics.BrowserRenderFailures.WithLabelValues(hostFromURL(rawURL), "navigate").Inc()
		return nil, err
	}
	pctx.MustWaitLoad()

	// Wait for network idle-ish.
	time.Sleep(r.cfg.WaitIdleTimeout)

	html, err := pctx.HTML()
	if err != nil {
		metrics.BrowserRenderFailures.WithLabelValues(hostFromURL(rawURL), "html").Inc()
		return nil, err
	}
	finalURL := pctx.MustInfo().URL

	return &Result{
		Body:     []byte(html),
		FinalURL: finalURL,
	}, nil
}

func (r *Renderer) newPage() (*rod.Page, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.browser == nil {
		return nil, fmt.Errorf("browser not initialized")
	}
	page, err := r.browser.Page(proto.TargetCreateTarget{})
	if err != nil {
		// try restart once
		if err := r.restart(); err != nil {
			return nil, err
		}
		return r.browser.Page(proto.TargetCreateTarget{})
	}
	return page, nil
}

func (r *Renderer) restart() error {
	if r.browser != nil {
		_ = r.browser.Close()
	}
	metrics.BrowserRestarts.Inc()
	l := launcher.New().Headless(r.cfg.Headless)
	if r.cfg.ExecutablePath != "" {
		l = l.Bin(r.cfg.ExecutablePath)
	}
	l = l.Append("--no-sandbox")
	return r.launch(l)
}

func (r *Renderer) launch(l *launcher.Launcher) error {
	if r.closed {
		return fmt.Errorf("renderer closed")
	}
	url, err := l.Launch()
	if err != nil {
		return fmt.Errorf("launch browser: %w", err)
	}
	b := rod.New().ControlURL(url)
	if err := b.Connect(); err != nil {
		return fmt.Errorf("connect browser: %w", err)
	}
	r.browser = b
	return nil
}

func (r *Renderer) acquireHost(ctx context.Context, rawURL string) (func(), error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return func() {}, err
	}
	host := u.Hostname()
	if host == "" {
		host = "default"
	}
	r.mu.Lock()
	ch, ok := r.hostSem[host]
	if !ok {
		ch = make(chan struct{}, r.cfg.PerHostLimit)
		r.hostSem[host] = ch
	}
	r.mu.Unlock()

	select {
	case ch <- struct{}{}:
		metrics.BrowserHostInFlight.WithLabelValues(host).Inc()
		return func() {
			r.releaseHostSem(host, ch)
		}, nil
	case <-ctx.Done():
		return func() {}, ctx.Err()
	}
}

func (r *Renderer) releaseHostSem(host string, ch chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-ch:
		if len(ch) == 0 {
			metrics.BrowserHostInFlight.DeleteLabelValues(host)
		} else {
			metrics.BrowserHostInFlight.WithLabelValues(host).Dec()
		}
	default:
	}
}

func (r *Renderer) recyclePage(p *pooledPage) {
	if p == nil || p.page == nil {
		return
	}
	maxLife := r.cfg.MaxPageLifetime
	if maxLife > 0 && time.Since(p.acquired) > maxLife {
		p.page.Close()
		return
	}
	select {
	case r.pagePool <- p:
	default:
		p.page.Close()
	}
}

func (r *Renderer) healthLoop() {
	interval := r.cfg.MaxPageLifetime
	if interval <= 0 {
		interval = time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		if r.closed {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), r.cfg.NavTimeout)
		metrics.BrowserHealthChecksTotal.Inc()
		err := r.Ready(ctx)
		cancel()
		if err != nil {
			metrics.BrowserHealth.Set(0)
			metrics.BrowserHealthFailuresTotal.Inc()
			metrics.BrowserRestarts.Inc()
			_ = r.restart()
		} else {
			metrics.BrowserHealth.Set(1)
		}
	}
}
