package format

import (
	"fmt"
	"strings"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/nickcecere/bullnose/internal/extract"
	stripmd "github.com/writeas/go-strip-markdown/v2"
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
		return markdownFromExtract(e)
	case FormatHTML:
		return e.HTML, nil
	case FormatText:
		mdText, err := markdownFromExtract(e)
		if err != nil {
			return "", err
		}
		text := stripmd.Strip(mdText)
		return strings.TrimSpace(text), nil
	default:
		return "", fmt.Errorf("unsupported format: %s", format)
	}
}

func markdownFromExtract(e *extract.Extracted) (string, error) {
	converter := md.NewConverter("", true, nil)
	mdText, err := converter.ConvertString(e.HTML)
	if err != nil {
		return "", err
	}
	mdText = strings.TrimSpace(mdText)
	if e.Title != "" && !strings.Contains(mdText, e.Title) {
		mdText = "# " + e.Title + "\n\n" + mdText
	}
	return mdText, nil
}
