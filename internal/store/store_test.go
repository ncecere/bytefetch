package store

import (
	"context"
	"testing"
)

type fakeDedupe struct {
	seen bool
}

func (f fakeDedupe) HasSeenCanonical(ctx context.Context, url string) (bool, error) {
	return f.seen, nil
}

func TestShouldSkipCanonical(t *testing.T) {
	checker := fakeDedupe{seen: true}
	skip, err := ShouldSkipCanonical(context.Background(), checker, "https://example.com")
	if err != nil {
		t.Fatalf("ShouldSkipCanonical returned error: %v", err)
	}
	if !skip {
		t.Fatalf("expected skip=true when seen")
	}
}

func TestMakeOrderClause(t *testing.T) {
	clause := makeOrderClause("ASC", "created_at")
	if clause != "created_at ASC, id ASC" {
		t.Fatalf("unexpected clause %s", clause)
	}
}
