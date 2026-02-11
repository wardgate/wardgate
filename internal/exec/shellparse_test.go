package exec

import (
	"errors"
	"testing"
)

func TestParseShellCommand_SimpleCommand(t *testing.T) {
	result, err := ParseShellCommand("echo hello world")
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
	result, err := ParseShellCommand("echo foo | head -20")
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
	result, err := ParseShellCommand("echo one && echo two")
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
	result, err := ParseShellCommand("true || echo fallback")
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
	result, err := ParseShellCommand("echo one ; echo two")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Segments) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(result.Segments))
	}
}

func TestParseShellCommand_Redirections(t *testing.T) {
	result, err := ParseShellCommand("echo hello > out.txt")
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

func TestParseShellCommand_RejectCommandSubstitution(t *testing.T) {
	_, err := ParseShellCommand("echo $(cat /etc/passwd)")
	if err == nil {
		t.Fatal("expected error for command substitution")
	}
	var unsafeErr *UnsafeShellError
	if !errors.As(err, &unsafeErr) {
		t.Errorf("expected UnsafeShellError, got %T: %v", err, err)
	}
}

func TestParseShellCommand_RejectBackticks(t *testing.T) {
	_, err := ParseShellCommand("echo `cat /etc/passwd`")
	if err == nil {
		t.Fatal("expected error for backtick substitution")
	}
	var unsafeErr *UnsafeShellError
	if !errors.As(err, &unsafeErr) {
		t.Errorf("expected UnsafeShellError, got %T: %v", err, err)
	}
}

func TestParseShellCommand_RejectProcessSubstitution(t *testing.T) {
	_, err := ParseShellCommand("diff <(echo a) <(echo b)")
	if err == nil {
		t.Fatal("expected error for process substitution")
	}
	var unsafeErr *UnsafeShellError
	if !errors.As(err, &unsafeErr) {
		t.Errorf("expected UnsafeShellError, got %T: %v", err, err)
	}
}

func TestParseShellCommand_RejectSubshells(t *testing.T) {
	_, err := ParseShellCommand("(cd /tmp && rm -rf *)")
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
	result, err := ParseShellCommand("echo '$(not a substitution)'")
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
	_, err := ParseShellCommand("")
	if err == nil {
		t.Fatal("expected error for empty command")
	}

	_, err = ParseShellCommand("   ")
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
