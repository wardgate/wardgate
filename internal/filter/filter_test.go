package filter

import (
	"testing"
)

func TestNewFilter(t *testing.T) {
	t.Run("default config", func(t *testing.T) {
		cfg := Config{
			Enabled: true,
		}
		f, err := New(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f == nil {
			t.Fatal("expected filter, got nil")
		}
		// Should have default patterns loaded
		if len(f.patterns) == 0 {
			t.Error("expected default patterns to be loaded")
		}
	})

	t.Run("disabled filter", func(t *testing.T) {
		cfg := Config{
			Enabled: false,
		}
		f, err := New(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if f == nil {
			t.Fatal("expected filter, got nil")
		}
		// Scan should return no matches when disabled
		matches := f.Scan("Your verification code is 123456")
		if len(matches) != 0 {
			t.Errorf("expected 0 matches when disabled, got %d", len(matches))
		}
	})

	t.Run("specific patterns only", func(t *testing.T) {
		cfg := Config{
			Enabled:  true,
			Patterns: []string{"otp_codes"}, // Only OTP codes, not verification links
		}
		f, err := New(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(f.patterns) != 1 {
			t.Errorf("expected 1 pattern, got %d", len(f.patterns))
		}
	})

	t.Run("invalid pattern name", func(t *testing.T) {
		cfg := Config{
			Enabled:  true,
			Patterns: []string{"nonexistent_pattern"},
		}
		_, err := New(cfg)
		if err == nil {
			t.Error("expected error for invalid pattern name")
		}
	})

	t.Run("custom patterns", func(t *testing.T) {
		cfg := Config{
			Enabled:  true,
			Patterns: []string{}, // No built-in patterns
			CustomPatterns: []CustomPattern{
				{
					Name:    "internal_id",
					Pattern: `INTERNAL-[A-Z0-9]{8}`,
				},
			},
		}
		f, err := New(cfg)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(f.patterns) != 1 {
			t.Errorf("expected 1 pattern, got %d", len(f.patterns))
		}
	})

	t.Run("invalid custom pattern regex", func(t *testing.T) {
		cfg := Config{
			Enabled: true,
			CustomPatterns: []CustomPattern{
				{
					Name:    "bad_regex",
					Pattern: `[invalid`,
				},
			},
		}
		_, err := New(cfg)
		if err == nil {
			t.Error("expected error for invalid regex")
		}
	})
}

func TestScanOTPCodes(t *testing.T) {
	f, err := New(Config{
		Enabled:  true,
		Patterns: []string{"otp_codes"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected int
		contains string
	}{
		{
			name:     "verification code 6 digits",
			input:    "Your verification code is 123456",
			expected: 1,
			contains: "123456",
		},
		{
			name:     "code with colon",
			input:    "Code: 847291",
			expected: 1,
			contains: "847291",
		},
		{
			name:     "OTP uppercase",
			input:    "Your OTP is 9876",
			expected: 1,
			contains: "9876",
		},
		{
			name:     "PIN code",
			input:    "PIN: 5432",
			expected: 1,
			contains: "5432",
		},
		{
			name:     "8 digit code",
			input:    "Your security code is 12345678",
			expected: 1,
			contains: "12345678",
		},
		{
			name:     "no code",
			input:    "Thank you for your order",
			expected: 0,
		},
		{
			name:     "number without context",
			input:    "There are 123456 items",
			expected: 0,
		},
		{
			name:     "too short",
			input:    "Code: 12",
			expected: 0,
		},
		{
			name:     "too long",
			input:    "Code: 1234567890",
			expected: 0,
		},
		{
			name:     "one-time code",
			input:    "Your one-time code is 654321",
			expected: 1,
			contains: "654321",
		},
		{
			name:     "2FA code",
			input:    "2FA code: 111222",
			expected: 1,
			contains: "111222",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := f.Scan(tt.input)
			if len(matches) != tt.expected {
				t.Errorf("expected %d matches, got %d", tt.expected, len(matches))
				for _, m := range matches {
					t.Logf("  match: %+v", m)
				}
			}
			if tt.expected > 0 && tt.contains != "" {
				found := false
				for _, m := range matches {
					if m.Value == tt.contains {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected match to contain %q", tt.contains)
				}
			}
		})
	}
}

func TestScanVerificationLinks(t *testing.T) {
	f, err := New(Config{
		Enabled:  true,
		Patterns: []string{"verification_links"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "verify link",
			input:    "Click here: https://example.com/verify/abc123",
			expected: 1,
		},
		{
			name:     "confirm link",
			input:    "Confirm: https://mail.example.com/confirm?token=xyz",
			expected: 1,
		},
		{
			name:     "reset password link",
			input:    "Reset password: https://auth.example.com/reset-password?code=abc",
			expected: 1,
		},
		{
			name:     "activate account",
			input:    "Activate your account: https://example.com/activate/user123",
			expected: 1,
		},
		{
			name:     "token parameter",
			input:    "https://example.com/action?token=secrettoken123",
			expected: 1,
		},
		{
			name:     "code parameter",
			input:    "https://example.com/login?code=authcode456",
			expected: 1,
		},
		{
			name:     "unsubscribe link",
			input:    "Unsubscribe: https://example.com/unsubscribe?id=123",
			expected: 1,
		},
		{
			name:     "normal link",
			input:    "Visit our website: https://example.com/about",
			expected: 0,
		},
		{
			name:     "link without sensitive path",
			input:    "https://example.com/blog/article-123",
			expected: 0,
		},
		{
			name:     "multiple verification links",
			input:    "Link 1: https://a.com/verify/x Link 2: https://b.com/confirm/y",
			expected: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := f.Scan(tt.input)
			if len(matches) != tt.expected {
				t.Errorf("expected %d matches, got %d", tt.expected, len(matches))
				for _, m := range matches {
					t.Logf("  match: %+v", m)
				}
			}
		})
	}
}

func TestScanAPIKeys(t *testing.T) {
	f, err := New(Config{
		Enabled:  true,
		Patterns: []string{"api_keys"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected int
	}{
		{
			name:     "OpenAI key",
			input:    "API key: sk-1234567890abcdefghijklmnop",
			expected: 1,
		},
		{
			name:     "GitHub token ghp",
			input:    "Token: ghp_abcdefghijklmnopqrstuvwxyz123456",
			expected: 1,
		},
		{
			name:     "GitHub token gho",
			input:    "OAuth: gho_abcdefghijklmnopqrstuvwxyz123456",
			expected: 1,
		},
		{
			name:     "Slack bot token",
			input:    "Bot token: xoxb-123-456-abcdefghij",
			expected: 1,
		},
		{
			name:     "Slack user token",
			input:    "User token: xoxp-123-456-789-abcdef",
			expected: 1,
		},
		{
			name:     "AWS access key",
			input:    "AWS key: AKIAIOSFODNN7EXAMPLE",
			expected: 1,
		},
		{
			name:     "no API key",
			input:    "This is just a normal message",
			expected: 0,
		},
		{
			name:     "sk- too short",
			input:    "sk-short",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := f.Scan(tt.input)
			if len(matches) != tt.expected {
				t.Errorf("expected %d matches, got %d", tt.expected, len(matches))
				for _, m := range matches {
					t.Logf("  match: %+v", m)
				}
			}
		})
	}
}

func TestScanAllPatterns(t *testing.T) {
	// Default config with all patterns enabled
	f, err := New(Config{Enabled: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := `
		Hi! Your verification code is 123456.
		Click here to verify: https://example.com/verify/abc
		Your API key is sk-abcdefghijklmnopqrstuvwxyz
	`

	matches := f.Scan(input)
	if len(matches) < 3 {
		t.Errorf("expected at least 3 matches, got %d", len(matches))
		for _, m := range matches {
			t.Logf("  match: %+v", m)
		}
	}

	// Verify we got different pattern types
	patternTypes := make(map[string]bool)
	for _, m := range matches {
		patternTypes[m.Pattern] = true
	}
	if !patternTypes["otp_codes"] {
		t.Error("expected otp_codes pattern match")
	}
	if !patternTypes["verification_links"] {
		t.Error("expected verification_links pattern match")
	}
	if !patternTypes["api_keys"] {
		t.Error("expected api_keys pattern match")
	}
}

func TestApply(t *testing.T) {
	f, err := New(Config{
		Enabled:     true,
		Patterns:    []string{"otp_codes"},
		Action:      ActionRedact,
		Replacement: "[REDACTED]",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single code",
			input:    "Your verification code is 123456",
			expected: "Your verification code is [REDACTED]",
		},
		{
			name:     "multiple codes",
			input:    "Code: 111111, backup code: 222222",
			expected: "Code: [REDACTED], backup code: [REDACTED]",
		},
		{
			name:     "no code",
			input:    "Hello world",
			expected: "Hello world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := f.Scan(tt.input)
			result := f.Apply(tt.input, matches)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestApplyWithCustomReplacement(t *testing.T) {
	f, err := New(Config{
		Enabled:     true,
		Patterns:    []string{"otp_codes"},
		Action:      ActionRedact,
		Replacement: "[CODE HIDDEN]",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	input := "Your code is 654321"
	matches := f.Scan(input)
	result := f.Apply(input, matches)

	expected := "Your code is [CODE HIDDEN]"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestShouldBlock(t *testing.T) {
	f, err := New(Config{
		Enabled:  true,
		Patterns: []string{"otp_codes"},
		Action:   ActionBlock,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "contains code",
			input:    "Your code is 123456",
			expected: true,
		},
		{
			name:     "no code",
			input:    "Hello world",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := f.Scan(tt.input)
			result := f.ShouldBlock(matches)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestShouldAsk(t *testing.T) {
	f, err := New(Config{
		Enabled:  true,
		Patterns: []string{"otp_codes"},
		Action:   ActionAsk,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "contains code",
			input:    "Your code is 123456",
			expected: true,
		},
		{
			name:     "no code",
			input:    "Hello world",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := f.Scan(tt.input)
			result := f.ShouldAsk(matches)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCustomPattern(t *testing.T) {
	f, err := New(Config{
		Enabled:  true,
		Patterns: []string{}, // No built-in patterns
		CustomPatterns: []CustomPattern{
			{
				Name:        "internal_id",
				Pattern:     `INTERNAL-[A-Z0-9]{8}`,
				Description: "Internal tracking IDs",
			},
		},
		Action:      ActionRedact,
		Replacement: "[INTERNAL ID]",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tests := []struct {
		name          string
		input         string
		expectMatches int
	}{
		{
			name:          "matches internal ID",
			input:         "Tracking: INTERNAL-ABC12345",
			expectMatches: 1,
		},
		{
			name:          "no match",
			input:         "External reference: EXT-12345",
			expectMatches: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := f.Scan(tt.input)
			if len(matches) != tt.expectMatches {
				t.Errorf("expected %d matches, got %d", tt.expectMatches, len(matches))
			}
		})
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.Enabled {
		t.Error("expected enabled by default")
	}
	if cfg.Action != ActionBlock {
		t.Errorf("expected default action to be block, got %v", cfg.Action)
	}
	if cfg.Replacement == "" {
		t.Error("expected default replacement to be set")
	}
	if len(cfg.Patterns) == 0 {
		t.Error("expected default patterns to be set")
	}
}

func TestFilterWithEmptyInput(t *testing.T) {
	f, err := New(Config{Enabled: true})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	matches := f.Scan("")
	if len(matches) != 0 {
		t.Errorf("expected 0 matches for empty input, got %d", len(matches))
	}

	result := f.Apply("", matches)
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
}

func TestOverlappingMatches(t *testing.T) {
	// Test that overlapping matches are handled correctly
	f, err := New(Config{
		Enabled:     true,
		Action:      ActionRedact,
		Replacement: "[X]",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// This input could potentially trigger overlapping matches
	input := "Code: 123456, verify at https://example.com/verify?code=123456"
	matches := f.Scan(input)

	// Apply should handle this without panicking
	result := f.Apply(input, matches)
	t.Logf("Input: %s", input)
	t.Logf("Result: %s", result)

	// The result should have some redactions
	if result == input {
		t.Error("expected some redactions in result")
	}
}
