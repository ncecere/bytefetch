package urlutil

import (
	"net/url"
	"path"
	"strings"
)

// Canonicalize normalizes a URL for deduping: lowercases scheme/host, strips default ports,
// cleans path, removes default index files, trims trailing slash (except root),
// removes fragment, and optionally strips query parameters.
func Canonicalize(raw string, stripQuery bool) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = stripDefaultPort(u)
	u.Fragment = ""
	if stripQuery {
		u.RawQuery = ""
	}
	u.Path = cleanPath(u.Path)
	return u.String(), nil
}

func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	cp := path.Clean(p)
	if cp == "." {
		cp = "/"
	}
	// drop default index files
	switch strings.ToLower(cp) {
	case "/index.html", "/index.htm", "/index.php":
		cp = "/"
	}
	if cp != "/" {
		cp = strings.TrimSuffix(cp, "/")
		if cp == "" {
			cp = "/"
		}
	}
	return cp
}

func stripDefaultPort(u *url.URL) string {
	if u == nil {
		return ""
	}
	host := u.Host
	switch strings.ToLower(u.Scheme) {
	case "http":
		host = strings.TrimSuffix(host, ":80")
	case "https":
		host = strings.TrimSuffix(host, ":443")
	}
	return strings.ToLower(host)
}
