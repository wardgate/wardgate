// Package filter provides sensitive data detection and filtering for Wardgate.
// It scans content for patterns like OTP codes, verification links, and API keys,
// and can redact or block content containing sensitive data.
package filter

import (
	"fmt"
	"regexp"
	"sort"
)

// Action defines what to do when sensitive data is detected.
type Action int

const (
	// ActionRedact replaces sensitive data with a placeholder.
	ActionRedact Action = iota
	// ActionBlock returns an error when sensitive data is detected.
	ActionBlock
	// ActionAsk requires human approval when sensitive data is detected.
	ActionAsk
	// ActionLog logs the detection but allows passthrough.
	ActionLog
)

// String returns the string representation of an Action.
func (a Action) String() string {
	switch a {
	case ActionRedact:
		return "redact"
	case ActionBlock:
		return "block"
	case ActionAsk:
		return "ask"
	case ActionLog:
		return "log"
	default:
		return "unknown"
	}
}

// ParseAction converts a string to an Action.
func ParseAction(s string) Action {
	switch s {
	case "redact":
		return ActionRedact
	case "block":
		return ActionBlock
	case "ask":
		return ActionAsk
	case "log":
		return ActionLog
	default:
		return ActionBlock // Default to block for safety
	}
}

// Config holds filter configuration.
type Config struct {
	Enabled        bool            `yaml:"enabled"`
	Patterns       []string        `yaml:"patterns,omitempty"`        // Built-in pattern names
	CustomPatterns []CustomPattern `yaml:"custom_patterns,omitempty"` // User-defined patterns
	Action         Action          `yaml:"action"`
	Replacement    string          `yaml:"replacement,omitempty"` // Replacement text for redact action
}

// CustomPattern defines a user-created pattern.
type CustomPattern struct {
	Name        string `yaml:"name"`
	Pattern     string `yaml:"pattern"` // Regex pattern
	Description string `yaml:"description,omitempty"`
}

// DefaultConfig returns the default filter configuration.
// By design, filtering is enabled by default with common sensitive patterns.
func DefaultConfig() Config {
	return Config{
		Enabled:     true,
		Patterns:    []string{"otp_codes", "verification_links", "api_keys"},
		Action:      ActionBlock,
		Replacement: "[SENSITIVE DATA REDACTED]",
	}
}

// Pattern is a compiled pattern for detecting sensitive data.
type Pattern struct {
	Name        string
	Regex       *regexp.Regexp
	Description string
}

// Match represents a detected piece of sensitive data.
type Match struct {
	Pattern string // Pattern name that matched
	Start   int    // Start position in content
	End     int    // End position in content
	Value   string // The matched value
}

// Filter scans and filters sensitive data from content.
type Filter struct {
	enabled     bool
	patterns    []*Pattern
	action      Action
	replacement string
}

// New creates a new Filter from configuration.
func New(cfg Config) (*Filter, error) {
	f := &Filter{
		enabled:     cfg.Enabled,
		action:      cfg.Action,
		replacement: cfg.Replacement,
	}

	if !cfg.Enabled {
		return f, nil
	}

	// Set defaults if not specified
	if f.replacement == "" {
		f.replacement = "[SENSITIVE DATA REDACTED]"
	}

	// Determine which patterns to load
	patternNames := cfg.Patterns
	if len(patternNames) == 0 && len(cfg.CustomPatterns) == 0 {
		// Use default patterns if none specified
		patternNames = []string{"otp_codes", "verification_links", "api_keys"}
	}

	// Load built-in patterns
	for _, name := range patternNames {
		builtin, ok := BuiltinPatterns[name]
		if !ok {
			return nil, fmt.Errorf("unknown pattern: %q", name)
		}
		f.patterns = append(f.patterns, builtin)
	}

	// Load custom patterns
	for _, cp := range cfg.CustomPatterns {
		regex, err := regexp.Compile(cp.Pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid regex for pattern %q: %w", cp.Name, err)
		}
		f.patterns = append(f.patterns, &Pattern{
			Name:        cp.Name,
			Regex:       regex,
			Description: cp.Description,
		})
	}

	return f, nil
}

// Scan searches content for sensitive data and returns all matches.
func (f *Filter) Scan(content string) []Match {
	if !f.enabled || content == "" {
		return nil
	}

	var matches []Match
	for _, p := range f.patterns {
		// Find all matches for this pattern
		locs := p.Regex.FindAllStringSubmatchIndex(content, -1)
		for _, loc := range locs {
			if len(loc) < 2 {
				continue
			}

			// Use the captured group if present, otherwise full match
			start, end := loc[0], loc[1]
			if len(loc) >= 4 && loc[2] >= 0 {
				// Use first capture group for the value
				start, end = loc[2], loc[3]
			}

			matches = append(matches, Match{
				Pattern: p.Name,
				Start:   start,
				End:     end,
				Value:   content[start:end],
			})
		}
	}

	// Sort by position (for Apply to work correctly)
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Start < matches[j].Start
	})

	return matches
}

// Apply replaces all matched sensitive data with the configured replacement.
// Returns the filtered content.
func (f *Filter) Apply(content string, matches []Match) string {
	if len(matches) == 0 {
		return content
	}

	// Build result by replacing matches in reverse order (to preserve positions)
	result := content
	seen := make(map[string]bool) // Track replaced positions to handle overlaps

	// Sort by position descending to replace from end to start
	sortedMatches := make([]Match, len(matches))
	copy(sortedMatches, matches)
	sort.Slice(sortedMatches, func(i, j int) bool {
		return sortedMatches[i].Start > sortedMatches[j].Start
	})

	for _, m := range sortedMatches {
		key := fmt.Sprintf("%d-%d", m.Start, m.End)
		if seen[key] {
			continue
		}
		seen[key] = true

		if m.Start >= 0 && m.End <= len(result) && m.Start < m.End {
			result = result[:m.Start] + f.replacement + result[m.End:]
		}
	}

	return result
}

// ShouldBlock returns true if the filter is configured to block and matches were found.
func (f *Filter) ShouldBlock(matches []Match) bool {
	return f.action == ActionBlock && len(matches) > 0
}

// ShouldAsk returns true if the filter is configured to ask and matches were found.
func (f *Filter) ShouldAsk(matches []Match) bool {
	return f.action == ActionAsk && len(matches) > 0
}

// Action returns the configured action for this filter.
func (f *Filter) Action() Action {
	return f.action
}

// Enabled returns whether the filter is enabled.
func (f *Filter) Enabled() bool {
	return f.enabled
}

// MatchDescription returns a human-readable description of the matches.
func MatchDescription(matches []Match) string {
	if len(matches) == 0 {
		return "no sensitive data detected"
	}

	patterns := make(map[string]int)
	for _, m := range matches {
		patterns[m.Pattern]++
	}

	desc := "sensitive data detected: "
	first := true
	for pattern, count := range patterns {
		if !first {
			desc += ", "
		}
		if count == 1 {
			desc += pattern
		} else {
			desc += fmt.Sprintf("%s (%d)", pattern, count)
		}
		first = false
	}
	return desc
}
