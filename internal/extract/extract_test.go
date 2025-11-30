package extract

import "testing"

func TestExtractMetadataAndLinks(t *testing.T) {
	html := `
	<!DOCTYPE html>
	<html lang="en">
	  <head>
		<title>Example Title</title>
		<meta name="description" content="Example description">
		<meta property="og:site_name" content="Example Site">
	  </head>
	  <body>
		<p>Hello world</p>
		<a href="/keep">Keep me</a>
		<a href="/skip" rel="nofollow">Skip me</a>
	  </body>
	</html>`

	ext, err := Extract("https://example.com/page", []byte(html))
	if err != nil {
		t.Fatalf("extract returned error: %v", err)
	}
	if ext.Meta["site_name"] != "Example Site" {
		t.Fatalf("expected site_name Example Site, got %q", ext.Meta["site_name"])
	}
	if ext.Meta["description"] != "Example description" {
		t.Fatalf("unexpected description %q", ext.Meta["description"])
	}
	if ext.Meta["language"] != "en" {
		t.Fatalf("expected language=en got %q", ext.Meta["language"])
	}
	if len(ext.Links) != 1 || ext.Links[0] != "/keep" {
		t.Fatalf("expected only one allowed link, got %v", ext.Links)
	}
	if ext.Title != "Example Title" {
		t.Fatalf("unexpected title %q", ext.Title)
	}
	if ext.Text == "" {
		t.Fatalf("expected non-empty text result")
	}
}
