package proxy

import (
	"net/url"
	"path"
	"strings"
)

// MatchUpstream checks if a target URL is allowed by any of the given patterns.
// Patterns are glob-style with scheme (e.g., "https://*.googleapis.com").
//
// Hostname glob rules:
//   - "*" matches exactly one hostname segment (between dots)
//   - "**" matches one or more hostname segments (across dots)
//
// Returns false for URLs with userinfo, non-HTTP schemes, or malformed input.
func MatchUpstream(rawURL string, patterns []string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	if u.User != nil {
		return false
	}

	if u.RawQuery != "" || u.Fragment != "" {
		return false
	}

	if u.Host == "" {
		return false
	}

	for _, pattern := range patterns {
		if matchUpstreamPattern(u, pattern) {
			return true
		}
	}

	return false
}

// matchUpstreamPattern matches a parsed URL against a single pattern.
// Pattern format: "https://*.example.com" or "https://**.example.com"
func matchUpstreamPattern(u *url.URL, pattern string) bool {
	// Replace ** first, then * with distinct placeholders for url.Parse.
	sanitized := strings.ReplaceAll(pattern, "**", "DOUBLEWILD")
	sanitized = strings.ReplaceAll(sanitized, "*", "SINGLEWILD")

	p, err := url.Parse(sanitized)
	if err != nil {
		return false
	}

	patternScheme := strings.ReplaceAll(p.Scheme, "DOUBLEWILD", "**")
	patternScheme = strings.ReplaceAll(patternScheme, "SINGLEWILD", "*")
	if patternScheme != u.Scheme {
		return false
	}

	patternHost := strings.ReplaceAll(p.Host, "DOUBLEWILD", "**")
	patternHost = strings.ReplaceAll(patternHost, "SINGLEWILD", "*")
	if !matchHostname(strings.ToLower(patternHost), strings.ToLower(u.Host)) {
		return false
	}

	patternPath := strings.ReplaceAll(p.Path, "DOUBLEWILD", "**")
	patternPath = strings.ReplaceAll(patternPath, "SINGLEWILD", "*")
	if patternPath != "" && patternPath != "/" {
		if u.Path != patternPath && !strings.HasPrefix(u.Path, patternPath+"/") {
			return false
		}
	}

	return true
}

// matchHostname matches a hostname against a pattern where:
//   - "*" matches exactly one dot-separated segment
//   - "**" matches one or more dot-separated segments
func matchHostname(pattern, host string) bool {
	// Fast path: no wildcards, use path.Match for simple glob chars (e.g., ?)
	if !strings.Contains(pattern, "*") {
		matched, err := path.Match(pattern, host)
		return err == nil && matched
	}

	patternParts := strings.Split(pattern, ".")
	hostParts := strings.Split(host, ".")

	return matchSegments(patternParts, hostParts)
}

// matchSegments recursively matches pattern segments against host segments.
func matchSegments(pattern, host []string) bool {
	for len(pattern) > 0 {
		seg := pattern[0]

		if seg == "**" {
			// ** must match at least one segment
			if len(host) == 0 {
				return false
			}
			rest := pattern[1:]
			// Try consuming 1..N host segments
			for i := 1; i <= len(host); i++ {
				if matchSegments(rest, host[i:]) {
					return true
				}
			}
			return false
		}

		if len(host) == 0 {
			return false
		}

		// Single segment match: * matches one segment, otherwise literal/glob
		matched, err := path.Match(seg, host[0])
		if err != nil || !matched {
			return false
		}

		pattern = pattern[1:]
		host = host[1:]
	}

	return len(host) == 0
}
