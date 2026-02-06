package filter

import "regexp"

// BuiltinPatterns contains the default sensitive data patterns.
// These are compiled at package init time for efficiency.
var BuiltinPatterns = map[string]*Pattern{
	"otp_codes": {
		Name:        "otp_codes",
		Description: "One-time passwords, verification codes, and PINs",
		// Matches codes 4-8 digits in context of keywords like "code", "OTP", "PIN", etc.
		// Allows words like "is" between keyword and code.
		// The capture group extracts just the digits.
		Regex: regexp.MustCompile(`(?i)(?:verification\s+code|one-time\s+code|security\s+code|2fa\s+code|otp|code|pin)(?:\s+is)?[:\s]+(\d{4,8})\b`),
	},

	"verification_links": {
		Name:        "verification_links",
		Description: "Email verification, password reset, and account activation links",
		// Matches URLs containing sensitive path segments or query parameters
		Regex: regexp.MustCompile(`https?://[^\s<>"]+(?:verify|confirm|reset|activate|unsubscribe|token=|code=|key=)[^\s<>"]*`),
	},

	"api_keys": {
		Name:        "api_keys",
		Description: "Common API key formats (OpenAI, GitHub, Slack, AWS, etc.)",
		// Matches common API key prefixes with sufficient length
		Regex: regexp.MustCompile(`(?:` +
			// OpenAI keys (sk-...)
			`sk-[a-zA-Z0-9]{20,}` +
			// GitHub tokens (ghp_, gho_, ghu_, ghs_, ghr_)
			`|gh[pousr]_[a-zA-Z0-9]{30,}` +
			// Slack tokens (xoxb-, xoxp-, xoxa-, xoxr-, xoxs-)
			`|xox[bpars]-[a-zA-Z0-9-]{10,}` +
			// AWS Access Key IDs (AKIA...)
			`|AKIA[0-9A-Z]{16}` +
			// Generic bearer/api tokens with common prefixes
			`|(?:bearer|api[_-]?key|api[_-]?token|secret[_-]?key)[:\s]+[a-zA-Z0-9_-]{20,}` +
			`)`),
	},

	"credit_cards": {
		Name:        "credit_cards",
		Description: "Credit card numbers (Visa, Mastercard, Amex, Discover)",
		// Matches common credit card formats (with or without separators)
		// Note: This is a basic pattern; real validation requires Luhn check
		Regex: regexp.MustCompile(`\b(?:` +
			// Visa: 4xxx xxxx xxxx xxxx
			`4[0-9]{3}[\s-]?[0-9]{4}[\s-]?[0-9]{4}[\s-]?[0-9]{4}` +
			// Mastercard: 5xxx xxxx xxxx xxxx
			`|5[1-5][0-9]{2}[\s-]?[0-9]{4}[\s-]?[0-9]{4}[\s-]?[0-9]{4}` +
			// Amex: 3xxx xxxxxx xxxxx
			`|3[47][0-9]{2}[\s-]?[0-9]{6}[\s-]?[0-9]{5}` +
			// Discover: 6xxx xxxx xxxx xxxx
			`|6(?:011|5[0-9]{2})[\s-]?[0-9]{4}[\s-]?[0-9]{4}[\s-]?[0-9]{4}` +
			`)\b`),
	},

	"passwords": {
		Name:        "passwords",
		Description: "Passwords in common formats (password: xxx, pwd=xxx)",
		// Matches password-like patterns in common formats
		Regex: regexp.MustCompile(`(?i)(?:password|passwd|pwd|secret)[:\s=]+["']?([^\s"'<>]{4,})["']?`),
	},

	"private_keys": {
		Name:        "private_keys",
		Description: "Private keys (SSH, PGP, RSA, etc.)",
		// Matches BEGIN/END blocks for private keys
		Regex: regexp.MustCompile(`-----BEGIN\s+(?:RSA\s+)?(?:PRIVATE|ENCRYPTED)\s+KEY-----`),
	},
}

// DefaultPatternNames returns the names of patterns enabled by default.
func DefaultPatternNames() []string {
	return []string{"otp_codes", "verification_links", "api_keys"}
}

// AllPatternNames returns all available builtin pattern names.
func AllPatternNames() []string {
	names := make([]string, 0, len(BuiltinPatterns))
	for name := range BuiltinPatterns {
		names = append(names, name)
	}
	return names
}
