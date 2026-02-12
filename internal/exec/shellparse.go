package exec

import (
	"fmt"
	"os/exec"
	"strings"
)

// ShellSegment represents a single command in a pipeline or chain.
type ShellSegment struct {
	Command  string // The command name (e.g., "rg", "head")
	Args     string // The joined argument string (e.g., "TODO src/")
	Resolved string // Absolute path of the resolved command (e.g., "/usr/bin/rg")
}

// ParseResult holds the result of parsing a shell command string.
type ParseResult struct {
	Segments []ShellSegment
	Raw      string // Original command string
}

// UnsafeShellError is returned when the command contains constructs
// that cannot be safely parsed (command substitution, subshells, etc.).
type UnsafeShellError struct {
	Reason string
}

func (e *UnsafeShellError) Error() string {
	return fmt.Sprintf("unsafe shell construct: %s", e.Reason)
}

// ParseOptions controls parsing behavior.
type ParseOptions struct {
	AllowRedirects bool // If false (default), reject shell redirections (>, >>, <, etc.)
}

// ParseShellCommand parses a command string into segments.
// It splits on pipes (|), chains (&&, ||, ;) and evaluates each segment.
// It rejects command substitution ($(), ``), process substitution (<(), >()),
// and subshells ((...)).
// If opts is nil, defaults are used (redirections rejected).
func ParseShellCommand(cmdStr string, opts *ParseOptions) (*ParseResult, error) {
	cmdStr = strings.TrimSpace(cmdStr)
	if cmdStr == "" {
		return nil, fmt.Errorf("empty command")
	}

	allowRedirects := false
	if opts != nil {
		allowRedirects = opts.AllowRedirects
	}

	// Reject unsafe constructs before parsing
	if err := checkUnsafeConstructs(cmdStr, allowRedirects); err != nil {
		return nil, err
	}

	// Split into segments on |, &&, ||, ;
	parts := splitShellSegments(cmdStr)

	segments := make([]ShellSegment, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		seg, err := parseSegment(part)
		if err != nil {
			return nil, err
		}
		segments = append(segments, seg)
	}

	if len(segments) == 0 {
		return nil, fmt.Errorf("no commands found")
	}

	return &ParseResult{
		Segments: segments,
		Raw:      cmdStr,
	}, nil
}

// checkUnsafeConstructs rejects shell constructs that introduce hidden command execution.
// If allowRedirects is false, shell redirections (>, >>, <, 2>, &>, etc.) are also rejected.
func checkUnsafeConstructs(s string, allowRedirects bool) error {
	// Walk through the string respecting quoting
	inSingle := false
	inDouble := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		// Track quoting state
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}

		// Skip checks inside single quotes (everything is literal)
		if inSingle {
			continue
		}

		// Check for $( command substitution
		if ch == '$' && i+1 < len(s) && s[i+1] == '(' {
			return &UnsafeShellError{Reason: "command substitution $() is not allowed"}
		}

		// Check for backtick command substitution
		if ch == '`' {
			return &UnsafeShellError{Reason: "backtick command substitution is not allowed"}
		}

		// Check for process substitution <() and >()
		if (ch == '<' || ch == '>') && i+1 < len(s) && s[i+1] == '(' {
			return &UnsafeShellError{Reason: "process substitution is not allowed"}
		}

		// Check for subshells: ( at start of a segment or after operator
		// We detect this by checking for ( that isn't part of $( or <( or >(
		if ch == '(' {
			// Check if preceded by $, <, or >
			if i > 0 && (s[i-1] == '$' || s[i-1] == '<' || s[i-1] == '>') {
				continue // Already caught above
			}
			return &UnsafeShellError{Reason: "subshells are not allowed"}
		}

		// Check for redirections (when not allowed)
		if !allowRedirects && !inDouble {
			if err := checkRedirection(s, i, ch); err != nil {
				return err
			}
		}
	}

	return nil
}

// checkRedirection checks if the character at position i is the start of a
// shell redirection operator. Returns an UnsafeShellError if so.
func checkRedirection(s string, i int, ch byte) *UnsafeShellError {
	switch ch {
	case '>':
		// Already handled >() as process substitution above; that check runs first.
		// This catches: >, >>, &>, &>>
		return &UnsafeShellError{Reason: "shell redirections are not allowed (use allow_redirects: true to enable)"}
	case '<':
		// Already handled <() as process substitution above.
		// This catches: <, <<
		return &UnsafeShellError{Reason: "shell redirections are not allowed (use allow_redirects: true to enable)"}
	}

	// Check for numeric redirects like 2>, 2>>
	if ch >= '0' && ch <= '9' && i+1 < len(s) && s[i+1] == '>' {
		// Make sure this is a fd redirect, not just a digit in an argument.
		// Only treat as redirect if preceded by whitespace or start of string.
		if i == 0 || s[i-1] == ' ' || s[i-1] == '\t' {
			return &UnsafeShellError{Reason: "shell redirections are not allowed (use allow_redirects: true to enable)"}
		}
	}

	// Check for &> and &>>
	if ch == '&' && i+1 < len(s) && s[i+1] == '>' {
		return &UnsafeShellError{Reason: "shell redirections are not allowed (use allow_redirects: true to enable)"}
	}

	return nil
}

// CheckRedirections checks if a command string contains shell redirections.
// Returns an UnsafeShellError if redirections are found.
// This is useful for server-side validation when allow_redirects is false.
func CheckRedirections(s string) error {
	inSingle := false
	inDouble := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if inSingle || inDouble {
			continue
		}

		// Skip process substitution markers (handled separately)
		if (ch == '<' || ch == '>') && i+1 < len(s) && s[i+1] == '(' {
			continue
		}

		if err := checkRedirection(s, i, ch); err != nil {
			return err
		}
	}

	return nil
}

// splitShellSegments splits a command string on |, &&, ||, and ;
// while respecting quoting.
func splitShellSegments(s string) []string {
	var segments []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		// Track quoting
		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			current.WriteByte(ch)
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			current.WriteByte(ch)
			continue
		}

		if inSingle || inDouble {
			current.WriteByte(ch)
			continue
		}

		// Check for operators (outside quotes)
		// && and ||
		if i+1 < len(s) {
			two := s[i : i+2]
			if two == "&&" || two == "||" {
				segments = append(segments, current.String())
				current.Reset()
				i++ // skip second char
				continue
			}
		}

		// | (but not ||, already handled)
		if ch == '|' {
			segments = append(segments, current.String())
			current.Reset()
			continue
		}

		// ;
		if ch == ';' {
			segments = append(segments, current.String())
			current.Reset()
			continue
		}

		current.WriteByte(ch)
	}

	if current.Len() > 0 {
		segments = append(segments, current.String())
	}

	return segments
}

// parseSegment parses a single command segment, stripping redirections
// and resolving the command to an absolute path.
func parseSegment(s string) (ShellSegment, error) {
	s = strings.TrimSpace(s)

	// Tokenize respecting quotes
	tokens := tokenize(s)
	if len(tokens) == 0 {
		return ShellSegment{}, fmt.Errorf("empty segment")
	}

	// Filter out redirections (>, >>, <, 2>, 2>&1, etc.) and their targets
	var cmdTokens []string
	for i := 0; i < len(tokens); i++ {
		t := tokens[i]
		if isRedirection(t) {
			// Skip the redirection and its target
			i++ // skip target
			continue
		}
		// Also skip tokens like "2>&1"
		if strings.Contains(t, ">&") {
			continue
		}
		cmdTokens = append(cmdTokens, t)
	}

	if len(cmdTokens) == 0 {
		return ShellSegment{}, fmt.Errorf("segment has no command after stripping redirections")
	}

	command := cmdTokens[0]
	args := ""
	if len(cmdTokens) > 1 {
		args = strings.Join(cmdTokens[1:], " ")
	}

	// Resolve command to absolute path
	resolved, err := resolveCommand(command)
	if err != nil {
		return ShellSegment{}, fmt.Errorf("cannot resolve command %q: %w", command, err)
	}

	return ShellSegment{
		Command:  command,
		Args:     args,
		Resolved: resolved,
	}, nil
}

// tokenize splits a command string into tokens, respecting quotes.
func tokenize(s string) []string {
	var tokens []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if ch == '\'' && !inDouble {
			inSingle = !inSingle
			// Don't include the quote in the token
			continue
		}
		if ch == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}

		if ch == ' ' && !inSingle && !inDouble {
			if current.Len() > 0 {
				tokens = append(tokens, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteByte(ch)
	}

	if current.Len() > 0 {
		tokens = append(tokens, current.String())
	}

	return tokens
}

// isRedirection checks if a token is a shell redirection operator.
func isRedirection(t string) bool {
	return t == ">" || t == ">>" || t == "<" || t == "2>" || t == "2>>" || t == "&>" || t == "&>>"
}

// resolveCommand resolves a command name to its absolute path.
// If the command is already an absolute path, it is returned as-is.
func resolveCommand(name string) (string, error) {
	if strings.HasPrefix(name, "/") {
		return name, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", err
	}
	// LookPath may return a relative path; ensure absolute
	if !strings.HasPrefix(path, "/") {
		return "", fmt.Errorf("resolved path %q is not absolute", path)
	}
	return path, nil
}
