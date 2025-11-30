package server

import "github.com/nickcecere/bullnose/internal/store"

func documentFromRow(r store.DocumentRow, job *store.Job, format string) documentResponse {
	meta := buildMeta(r.Meta, job, r.RequestedURL, r.FinalURL)
	return documentResponse{
		ID:      r.ID,
		JobID:   r.JobID,
		Content: r.ContentMD,
		Format:  format,
		Meta:    meta,
	}
}
