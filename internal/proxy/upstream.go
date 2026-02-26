package proxy

import (
	"net/url"
	"path"
	"strings"
)

// MatchUpstream checks if a target URL is allowed by any of the given patterns.
// Patterns are glob-style with scheme (e.g., "https://*.googleapis.com").
// Returns false for URLs with userinfo, non-HTTP schemes, or malformed input.
func MatchUpstream(rawURL string, patterns []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// Reject non-HTTP schemes
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	// Reject userinfo (SSRF vector: http://user@internal-host)
	if u.User != nil {
		return false
	}

	// Reject URLs with query parameters or fragments (keep upstream clean)
	if u.RawQuery != "" || u.Fragment != "" {
		return false
	}

	// Must have a host
	if u.Host == "" {
		return false
	}

	// Match against each pattern
	for _, pattern := range patterns {
		if matchUpstreamPattern(u, pattern) {
			return true
		}
	}

	return false
}

// matchUpstreamPattern matches a parsed URL against a single pattern.
// Pattern format: "https://*.example.com" or "https://api.example.com"
func matchUpstreamPattern(u *url.URL, pattern string) bool {
	// Replace * with WILDCARD so url.Parse doesn't treat globs as invalid path chars.
	p, err := url.Parse(strings.ReplaceAll(pattern, "*", "WILDCARD"))
	if err != nil {
		return false
	}

	// Scheme must match exactly
	patternScheme := strings.ReplaceAll(p.Scheme, "WILDCARD", "*")
	if patternScheme != u.Scheme {
		return false
	}

	// Match hostname using path.Match (glob matching where * matches any non-/ chars).
	// Since hostnames contain no /, * matches across dots (e.g., *.example.com).
	// Normalize to lowercase because DNS hostnames are case-insensitive.
	patternHost := strings.ToLower(strings.ReplaceAll(p.Host, "WILDCARD", "*"))
	matched, err := path.Match(patternHost, strings.ToLower(u.Host))
	if err != nil || !matched {
		return false
	}

	// If pattern has a path beyond "/", the URL path must match exactly or
	// as a segment prefix (e.g., /v1 matches /v1/foo but not /v1-admin).
	patternPath := strings.ReplaceAll(p.Path, "WILDCARD", "*")
	if patternPath != "" && patternPath != "/" {
		if u.Path != patternPath && !strings.HasPrefix(u.Path, patternPath+"/") {
			return false
		}
	}

	return true
}
