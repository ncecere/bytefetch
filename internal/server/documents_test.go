package server

import (
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/ncecere/bullnose/internal/store"
)

func TestDocumentFromRow(t *testing.T) {
	job := &store.Job{
		ID:        "job-1",
		ExpiresAt: sqlNullTime(t),
	}
	meta := map[string]any{"title": "Example"}
	raw, _ := json.Marshal(meta)
	row := store.DocumentRow{
		ID:           1,
		JobID:        "job-1",
		RequestedURL: "https://example.com",
		FinalURL:     "https://example.com",
		ContentMD:    "body",
		Meta:         raw,
	}
	resp := documentFromRow(row, job, []string{"text"})
	if len(resp.Outputs) != 1 || resp.Outputs[0].Format != "text" {
		t.Fatalf("expected single output honoring preferred format, got %#v", resp.Outputs)
	}
	metaResp, ok := resp.Meta.(map[string]any)
	if !ok {
		t.Fatalf("meta not map: %#v", resp.Meta)
	}
	if metaResp["requested_url"] != "https://example.com" {
		t.Fatalf("requested_url missing: %#v", metaResp)
	}
	if metaResp["expires_at"] == "" {
		t.Fatalf("expires_at missing: %#v", metaResp)
	}
}

func sqlNullTime(t *testing.T) sql.NullTime {
	t.Helper()
	return sql.NullTime{Valid: true, Time: time.Now()}
}
