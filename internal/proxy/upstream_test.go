package proxy

import "testing"

func TestMatchUpstream_ExactMatch(t *testing.T) {
	patterns := []string{"https://api.github.com"}

	if !MatchUpstream("https://api.github.com", patterns) {
		t.Error("expected exact match to succeed")
	}
	if !MatchUpstream("https://api.github.com/repos", patterns) {
		t.Error("expected non-root path to be allowed (no path restriction in pattern)")
	}
}

func TestMatchUpstream_GlobSubdomain(t *testing.T) {
	patterns := []string{"https://*.googleapis.com"}

	tests := []struct {
		url  string
		want bool
	}{
		{"https://storage.googleapis.com", true},
		{"https://compute.googleapis.com", true},
		{"https://googleapis.com", false},
		{"https://evil.com.googleapis.com", false}, // * matches one segment only
		{"http://storage.googleapis.com", false},   // scheme mismatch
	}

	for _, tt := range tests {
		got := MatchUpstream(tt.url, patterns)
		if got != tt.want {
			t.Errorf("MatchUpstream(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestMatchUpstream_DoubleStarSubdomain(t *testing.T) {
	patterns := []string{"https://**.googleapis.com"}

	tests := []struct {
		url  string
		want bool
	}{
		{"https://storage.googleapis.com", true},
		{"https://compute.googleapis.com", true},
		{"https://googleapis.com", false},                     // ** requires at least one segment
		{"https://intended.com.googleapis.com", true},         // ** matches multiple segments
		{"https://deep.nested.sub.googleapis.com", true},      // ** matches many segments
		{"http://storage.googleapis.com", false},              // scheme mismatch
		{"https://storage.googleapis.com/some/path", true},    // path allowed
	}

	for _, tt := range tests {
		got := MatchUpstream(tt.url, patterns)
		if got != tt.want {
			t.Errorf("MatchUpstream(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestMatchUpstream_SchemeMismatch(t *testing.T) {
	patterns := []string{"https://api.example.com"}

	if MatchUpstream("http://api.example.com", patterns) {
		t.Error("http should not match https pattern")
	}
}

func TestMatchUpstream_SSRFVectors(t *testing.T) {
	patterns := []string{"https://*.example.com"}

	tests := []struct {
		name string
		url  string
	}{
		{"userinfo", "https://admin:password@internal.example.com"},
		{"query params", "https://api.example.com?redirect=http://evil.com"},
		{"fragment", "https://api.example.com#something"},
		{"non-http scheme", "ftp://api.example.com"},
		{"javascript scheme", "javascript://api.example.com"},
		{"file scheme", "file:///etc/passwd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if MatchUpstream(tt.url, patterns) {
				t.Errorf("expected SSRF vector %q to be rejected", tt.url)
			}
		})
	}
}

func TestMatchUpstream_MalformedURLs(t *testing.T) {
	patterns := []string{"https://*.example.com"}

	tests := []struct {
		name string
		url  string
	}{
		{"empty string", ""},
		{"no scheme", "api.example.com"},
		{"no host", "https://"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if MatchUpstream(tt.url, patterns) {
				t.Errorf("expected malformed URL %q to be rejected", tt.url)
			}
		})
	}
}

func TestMatchUpstream_MultiplePatterns(t *testing.T) {
	patterns := []string{
		"https://api.github.com",
		"https://uploads.github.com",
	}

	if !MatchUpstream("https://api.github.com", patterns) {
		t.Error("expected first pattern to match")
	}
	if !MatchUpstream("https://uploads.github.com", patterns) {
		t.Error("expected second pattern to match")
	}
	if MatchUpstream("https://gist.github.com", patterns) {
		t.Error("expected non-matching URL to be rejected")
	}
}

func TestMatchUpstream_NoPatterns(t *testing.T) {
	if MatchUpstream("https://api.example.com", nil) {
		t.Error("expected nil patterns to reject everything")
	}
	if MatchUpstream("https://api.example.com", []string{}) {
		t.Error("expected empty patterns to reject everything")
	}
}

func TestMatchUpstream_PathInPattern(t *testing.T) {
	patterns := []string{"https://api.example.com/v1"}

	if !MatchUpstream("https://api.example.com/v1", patterns) {
		t.Error("expected path match")
	}
	if MatchUpstream("https://api.example.com/v2", patterns) {
		t.Error("expected path mismatch to be rejected")
	}
}

func TestMatchUpstream_CaseInsensitive(t *testing.T) {
	patterns := []string{"https://api.example.com"}

	tests := []struct {
		url  string
		want bool
	}{
		{"https://API.EXAMPLE.COM", true},
		{"https://Api.Example.Com", true},
		{"https://api.EXAMPLE.com/path", true},
	}

	for _, tt := range tests {
		got := MatchUpstream(tt.url, patterns)
		if got != tt.want {
			t.Errorf("MatchUpstream(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestMatchUpstream_PathSegmentBoundary(t *testing.T) {
	patterns := []string{"https://api.example.com/v1"}

	tests := []struct {
		url  string
		want bool
	}{
		{"https://api.example.com/v1", true},
		{"https://api.example.com/v1/users", true},
		{"https://api.example.com/v1-admin", false},
		{"https://api.example.com/v1beta", false},
		{"https://api.example.com/v10", false},
	}

	for _, tt := range tests {
		got := MatchUpstream(tt.url, patterns)
		if got != tt.want {
			t.Errorf("MatchUpstream(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}

func TestMatchUpstream_PortHandling(t *testing.T) {
	patterns := []string{"https://api.example.com:8443"}

	tests := []struct {
		url  string
		want bool
	}{
		{"https://api.example.com:8443", true},
		{"https://api.example.com:8443/path", true},
		{"https://api.example.com:443", false},
		{"https://api.example.com", false},
	}

	for _, tt := range tests {
		got := MatchUpstream(tt.url, patterns)
		if got != tt.want {
			t.Errorf("MatchUpstream(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}
