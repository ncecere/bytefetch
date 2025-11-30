package worker

import (
	"context"
	"net/url"
	"sync"

	"golang.org/x/time/rate"
)

type hostLimiter struct {
	maxConc int
	rps     int

	mu        sync.Mutex
	conc      map[string]chan struct{}
	rateLimit map[string]*rate.Limiter
}

func newHostLimiter(maxConc, rps int) *hostLimiter {
	return &hostLimiter{
		maxConc:   maxConc,
		rps:       rps,
		conc:      make(map[string]chan struct{}),
		rateLimit: make(map[string]*rate.Limiter),
	}
}

// acquire enforces per-host concurrency and rate limits. release must be called.
func (h *hostLimiter) acquire(ctx context.Context, rawURL string) (func(), error) {
	host := extractHost(rawURL)
	if host == "" {
		host = "default"
	}

	if h.maxConc > 0 {
		ch := h.getConc(host)
		select {
		case ch <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if h.rps > 0 {
		lim := h.getLimiter(host)
		if err := lim.Wait(ctx); err != nil {
			if h.maxConc > 0 {
				h.releaseConc(host)
			}
			return nil, err
		}
	}

	return func() {
		if h.maxConc > 0 {
			h.releaseConc(host)
		}
	}, nil
}

func (h *hostLimiter) getConc(host string) chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch, ok := h.conc[host]
	if !ok {
		ch = make(chan struct{}, h.maxConc)
		h.conc[host] = ch
	}
	return ch
}

func (h *hostLimiter) getLimiter(host string) *rate.Limiter {
	h.mu.Lock()
	defer h.mu.Unlock()
	lim, ok := h.rateLimit[host]
	if !ok {
		lim = rate.NewLimiter(rate.Limit(h.rps), h.rps)
		h.rateLimit[host] = lim
	}
	return lim
}

func (h *hostLimiter) releaseConc(host string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if ch, ok := h.conc[host]; ok {
		select {
		case <-ch:
		default:
		}
	}
}

func extractHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
