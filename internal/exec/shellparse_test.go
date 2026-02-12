package exec

import (
	"errors"
	"testing"
)

// allowRedirects is a convenience for tests that need redirections permitted.
var allowRedirects = &ParseOptions{AllowRedirects: true}

func TestParseShellCommand_SimpleCommand(t *testing.T) {
	result, err := ParseShellCommand("echo hello world", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(result.Segments))
	}
	if result.Segments[0].Command != "echo" {
		t.Errorf("expected command 'echo', got %q", result.Segments[0].Command)
	}
	if result.Segments[0].Args != "hello world" {
		t.Errorf("expected args 'hello world', got %q", result.Segments[0].Args)
	}
	if result.Raw != "echo hello world" {
		t.Errorf("expected raw 'echo hello world', got %q", result.Raw)
	}
}

func TestParseShellCommand_Pipeline(t *testing.T) {
	result, err := ParseShellCommand("echo foo | head -20", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.Segments))
	}
	if result.Segments[0].Command != "echo" {
		t.Errorf("segment 0: expected command 'echo', got %q", result.Segments[0].Command)
	}
	if result.Segments[1].Command != "head" {
		t.Errorf("segment 1: expected command 'head', got %q", result.Segments[1].Command)
	}
	if result.Segments[1].Args != "-20" {
		t.Errorf("segment 1: expected args '-20', got %q", result.Segments[1].Args)
	}
}

func TestParseShellCommand_Chain(t *testing.T) {
	result, err := ParseShellCommand("echo one && echo two", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.Segments))
	}
	if result.Segments[0].Command != "echo" {
		t.Errorf("segment 0: expected 'echo', got %q", result.Segments[0].Command)
	}
	if result.Segments[1].Command != "echo" {
		t.Errorf("segment 1: expected 'echo', got %q", result.Segments[1].Command)
	}
}

func TestParseShellCommand_OrChain(t *testing.T) {
	result, err := ParseShellCommand("true || echo fallback", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.Segments))
	}
	if result.Segments[0].Command != "true" {
		t.Errorf("segment 0: expected 'true', got %q", result.Segments[0].Command)
	}
	if result.Segments[1].Command != "echo" {
		t.Errorf("segment 1: expected 'echo', got %q", result.Segments[1].Command)
	}
}

func TestParseShellCommand_Semicolon(t *testing.T) {
	result, err := ParseShellCommand("echo one ; echo two", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.Segments))
	}
}

func TestParseShellCommand_RejectRedirections(t *testing.T) {
	// Redirections are rejected by default (nil options)
	cases := []string{
		"echo hello > out.txt",
		"echo hello >> out.txt",
		"cat < input.txt",
		"cmd 2> err.log",
		"cmd 2>> err.log",
		"cmd &> all.log",
		"cmd &>> all.log",
	}
	for _, c := range cases {
		_, err := ParseShellCommand(c, nil)
		if err == nil {
			t.Errorf("expected error for %q, got nil", c)
			continue
		}
		var unsafeErr *UnsafeShellError
		if !errors.As(err, &unsafeErr) {
			t.Errorf("expected UnsafeShellError for %q, got %T: %v", c, err, err)
		}
	}
}

func TestParseShellCommand_AllowRedirections(t *testing.T) {
	// Redirections are allowed when opted in
	result, err := ParseShellCommand("echo hello > out.txt", allowRedirects)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(result.Segments))
	}
	if result.Segments[0].Command != "echo" {
		t.Errorf("expected command 'echo', got %q", result.Segments[0].Command)
	}
	if result.Segments[0].Args != "hello" {
		t.Errorf("expected args 'hello' (redirection stripped), got %q", result.Segments[0].Args)
	}
}

func TestParseShellCommand_RedirectionsInQuotesOK(t *testing.T) {
	// Redirections inside single quotes are literal, not operators
	result, err := ParseShellCommand("echo '> not a redirect'", nil)
	if err != nil {
		t.Fatalf("single-quoted > should be safe, got error: %v", err)
	}
	if len(result.Segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(result.Segments))
	}
}

func TestParseShellCommand_PipelineWithRedirectRejected(t *testing.T) {
	// Pipeline containing a redirect should be rejected by default
	_, err := ParseShellCommand("echo secret > /tmp/exfil.txt | true", nil)
	if err == nil {
		t.Fatal("expected error for pipeline with redirect")
	}
	var unsafeErr *UnsafeShellError
	if !errors.As(err, &unsafeErr) {
		t.Errorf("expected UnsafeShellError, got %T: %v", err, err)
	}
}

func TestCheckRedirections(t *testing.T) {
	// Should detect redirections
	if err := CheckRedirections("echo hello > out.txt"); err == nil {
		t.Error("expected error for >")
	}
	if err := CheckRedirections("cat < in.txt"); err == nil {
		t.Error("expected error for <")
	}
	// Should pass without redirections
	if err := CheckRedirections("echo hello world"); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	// Quoted redirections should be fine
	if err := CheckRedirections("echo '> not a redirect'"); err != nil {
		t.Errorf("quoted > should be safe: %v", err)
	}
}

func TestParseShellCommand_RejectCommandSubstitution(t *testing.T) {
	_, err := ParseShellCommand("echo $(cat /etc/passwd)", nil)
	if err == nil {
		t.Fatal("expected error for command substitution")
	}
	var unsafeErr *UnsafeShellError
	if !errors.As(err, &unsafeErr) {
		t.Errorf("expected UnsafeShellError, got %T: %v", err, err)
	}
}

func TestParseShellCommand_RejectBackticks(t *testing.T) {
	_, err := ParseShellCommand("echo `cat /etc/passwd`", nil)
	if err == nil {
		t.Fatal("expected error for backtick substitution")
	}
	var unsafeErr *UnsafeShellError
	if !errors.As(err, &unsafeErr) {
		t.Errorf("expected UnsafeShellError, got %T: %v", err, err)
	}
}

func TestParseShellCommand_RejectProcessSubstitution(t *testing.T) {
	_, err := ParseShellCommand("diff <(echo a) <(echo b)", nil)
	if err == nil {
		t.Fatal("expected error for process substitution")
	}
	var unsafeErr *UnsafeShellError
	if !errors.As(err, &unsafeErr) {
		t.Errorf("expected UnsafeShellError, got %T: %v", err, err)
	}
}

func TestParseShellCommand_RejectSubshells(t *testing.T) {
	_, err := ParseShellCommand("(cd /tmp && rm -rf *)", nil)
	if err == nil {
		t.Fatal("expected error for subshell")
	}
	var unsafeErr *UnsafeShellError
	if !errors.As(err, &unsafeErr) {
		t.Errorf("expected UnsafeShellError, got %T: %v", err, err)
	}
}

func TestParseShellCommand_QuotedSafe(t *testing.T) {
	// Single-quoted $() should be treated as literal, not rejected
	result, err := ParseShellCommand("echo '$(not a substitution)'", nil)
	if err != nil {
		t.Fatalf("single-quoted $() should be safe, got error: %v", err)
	}
	if len(result.Segments) != 1 {
		t.Fatalf("expected 1 segment, got %d", len(result.Segments))
	}
	if result.Segments[0].Command != "echo" {
		t.Errorf("expected command 'echo', got %q", result.Segments[0].Command)
	}
}

func TestParseShellCommand_Empty(t *testing.T) {
	_, err := ParseShellCommand("", nil)
	if err == nil {
		t.Fatal("expected error for empty command")
	}

	_, err = ParseShellCommand("   ", nil)
	if err == nil {
		t.Fatal("expected error for whitespace-only command")
	}
}

func TestTokenize(t *testing.T) {
	tests := []struct {
		input  string
		expect []string
	}{
		{"hello world", []string{"hello", "world"}},
		{"echo 'hello world'", []string{"echo", "hello world"}},
		{`echo "hello world"`, []string{"echo", "hello world"}},
		{"a  b  c", []string{"a", "b", "c"}},
		{"", nil},
		{"single", []string{"single"}},
		{`echo "it's fine"`, []string{"echo", "it's fine"}},
	}

	for _, tt := range tests {
		got := tokenize(tt.input)
		if len(got) != len(tt.expect) {
			t.Errorf("tokenize(%q): expected %d tokens, got %d: %v", tt.input, len(tt.expect), len(got), got)
			continue
		}
		for i := range got {
			if got[i] != tt.expect[i] {
				t.Errorf("tokenize(%q)[%d]: expected %q, got %q", tt.input, i, tt.expect[i], got[i])
			}
		}
	}
}

func TestIsRedirection(t *testing.T) {
	redirections := []string{">", ">>", "<", "2>", "2>>", "&>", "&>>"}
	for _, r := range redirections {
		if !isRedirection(r) {
			t.Errorf("expected %q to be a redirection", r)
		}
	}

	nonRedirections := []string{"echo", "-", "|", "&&", "||", "file.txt"}
	for _, r := range nonRedirections {
		if isRedirection(r) {
			t.Errorf("expected %q to NOT be a redirection", r)
		}
	}
}

func TestSplitShellSegments(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"echo hello", 1},
		{"echo a | echo b", 2},
		{"echo a && echo b", 2},
		{"echo a || echo b", 2},
		{"echo a ; echo b", 2},
		{"echo a | echo b | echo c", 3},
		{"echo a && echo b || echo c", 3},
		{"echo 'a | b'", 1},   // pipe inside quotes
		{`echo "a && b"`, 1},  // chain inside quotes
	}

	for _, tt := range tests {
		segments := splitShellSegments(tt.input)
		if len(segments) != tt.expected {
			t.Errorf("splitShellSegments(%q): expected %d segments, got %d: %v", tt.input, tt.expected, len(segments), segments)
		}
	}
}
