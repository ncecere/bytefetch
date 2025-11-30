package store

import "context"

// DedupeChecker exposes HasSeenCanonical for helpers outside the store package.
type DedupeChecker interface {
	HasSeenCanonical(ctx context.Context, url string) (bool, error)
}

// ShouldSkipCanonical returns true if the canonical URL has been seen already.
func ShouldSkipCanonical(ctx context.Context, checker DedupeChecker, canonicalURL string) (bool, error) {
	if canonicalURL == "" {
		return false, nil
	}
	return checker.HasSeenCanonical(ctx, canonicalURL)
}
