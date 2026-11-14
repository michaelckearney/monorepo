package favicon

import (
	"net/url"
	"strings"

	"golang.org/x/net/idna"
)

// Origin reduces a service URL to the scheme-host-port triple used as the icon
// cache key. It returns "" for anything that cannot host a favicon, which
// callers read as "this service has no icon".
//
// The rules deliberately match JavaScript's `new URL(x).origin`, because the
// frontend computes the same key when it looks an icon up. A mismatch between
// the two would not fail loudly — every lookup would simply miss, and the
// feature would silently render nothing. That is why the two odd normalizations
// below exist: Go's net/url and the WHATWG URL parser disagree about them, and
// agreement matters more here than either interpretation does.
//
// Service URLs come from a user-authored document, so a missing scheme or a
// mailto: is ordinary input rather than an error.
func Origin(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(normalizeAuthoritySlashes(trimmed))
	if err != nil {
		return ""
	}
	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "http" && scheme != "https" {
		return ""
	}
	host := asciiHost(parsed.Hostname())
	if host == "" {
		return ""
	}
	// url.Hostname strips the brackets that make an IPv6 literal unambiguous;
	// the origin has to put them back or the result is not a usable URL.
	if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	port := parsed.Port()
	if port == defaultPort(scheme) {
		port = ""
	}
	if port != "" {
		return scheme + "://" + host + ":" + port
	}
	return scheme + "://" + host
}

// normalizeAuthoritySlashes rewrites any number of slashes after an http(s)
// scheme to exactly two.
//
// The WHATWG parser does this for special schemes, so it reads "https:/host",
// "https:host", and "https:///host" all as authorities. net/url reads the first
// two as opaque and the third as a path with no host. Left alone, a typo in a
// service URL would produce an origin in the UI that the backend never cached
// under.
func normalizeAuthoritySlashes(raw string) string {
	lowered := strings.ToLower(raw)
	var scheme string
	switch {
	case strings.HasPrefix(lowered, "https:"):
		scheme = "https:"
	case strings.HasPrefix(lowered, "http:"):
		scheme = "http:"
	default:
		return raw
	}
	return scheme + "//" + strings.TrimLeft(raw[len(scheme):], "/")
}

// asciiHost lowercases a host and converts an internationalized one to its
// punycode form, which is what `new URL(x).origin` reports.
//
// A host the IDNA profile rejects — an underscore, a trailing dot — falls back
// to the lowercased original, which is also what the WHATWG parser keeps.
func asciiHost(host string) string {
	if host == "" {
		return ""
	}
	lowered := strings.ToLower(host)
	if !hasNonASCII(lowered) {
		return lowered
	}
	converted, err := idna.Lookup.ToASCII(lowered)
	if err != nil {
		return lowered
	}
	return converted
}

func hasNonASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] >= 0x80 {
			return true
		}
	}
	return false
}

func defaultPort(scheme string) string {
	if scheme == "https" {
		return "443"
	}
	return "80"
}
