package format

import (
	"fmt"
	"strings"

	"github.com/nickcecere/bullnose/internal/extract"
)

const (
	FormatMarkdown = "markdown"
	FormatHTML     = "html"
	FormatText     = "text"
)

// Format returns the content in the requested format.
func FormatContent(e *extract.Extracted, format string) (string, error) {
	switch strings.ToLower(format) {
	case FormatMarkdown:
		var b strings.Builder
		if e.Title != "" {
			b.WriteString("# ")
			b.WriteString(e.Title)
			b.WriteString("\n\n")
		}
		b.WriteString(e.Text)
		return b.String(), nil
	case FormatHTML:
		return e.HTML, nil
	case FormatText:
		return e.Text, nil
	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}
}
