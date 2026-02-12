package exec

import (
	"strings"
	"testing"

	"github.com/wardgate/wardgate/internal/config"
)

func TestExpandTemplate_Basic(t *testing.T) {
	tests := []struct {
		name     string
		template string
		args     []config.CommandArg
		values   []string
		want     string
	}{
		{
			name:     "single arg",
			template: "find . -iname {query}",
			args:     []config.CommandArg{{Name: "query"}},
			values:   []string{"*.md"},
			want:     "find . -iname '*.md'",
		},
		{
			name:     "arg in pipeline",
			template: "rg {pattern} | grep -v SECRET1 | grep -v SECRET2",
			args:     []config.CommandArg{{Name: "pattern"}},
			values:   []string{"TODO"},
			want:     "rg 'TODO' | grep -v SECRET1 | grep -v SECRET2",
		},
		{
			name:     "multiple args",
			template: "find {dir} -name {pattern}",
			args:     []config.CommandArg{{Name: "dir"}, {Name: "pattern"}},
			values:   []string{"/tmp", "*.log"},
			want:     "find '/tmp' -name '*.log'",
		},
		{
			name:     "no args",
			template: "date +%Y-%m-%d",
			args:     nil,
			values:   nil,
			want:     "date +%Y-%m-%d",
		},
		{
			name:     "arg used twice",
			template: "echo {word} && echo {word}",
			args:     []config.CommandArg{{Name: "word"}},
			values:   []string{"hello"},
			want:     "echo 'hello' && echo 'hello'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandTemplate(tt.template, tt.args, tt.values)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExpandTemplate_ShellEscaping(t *testing.T) {
	args := []config.CommandArg{{Name: "input"}}

	tests := []struct {
		name  string
		value string
		want  string // expected escaped value inside the template "echo {input}"
	}{
		{
			name:  "spaces",
			value: "hello world",
			want:  "echo 'hello world'",
		},
		{
			name:  "single quotes",
			value: "it's here",
			want:  "echo 'it'\\''s here'",
		},
		{
			name:  "semicolon injection",
			value: "foo; rm -rf /",
			want:  "echo 'foo; rm -rf /'",
		},
		{
			name:  "backtick injection",
			value: "`whoami`",
			want:  "echo '`whoami`'",
		},
		{
			name:  "dollar substitution",
			value: "$(cat /etc/passwd)",
			want:  "echo '$(cat /etc/passwd)'",
		},
		{
			name:  "double quotes",
			value: `say "hello"`,
			want:  `echo 'say "hello"'`,
		},
		{
			name:  "newlines",
			value: "line1\nline2",
			want:  "echo 'line1\nline2'",
		},
		{
			name:  "pipe injection",
			value: "foo | rm -rf /",
			want:  "echo 'foo | rm -rf /'",
		},
		{
			name:  "empty string",
			value: "",
			want:  "echo ''",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExpandTemplate("echo {input}", args, []string{tt.value})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExpandTemplate_Errors(t *testing.T) {
	t.Run("too few values", func(t *testing.T) {
		_, err := ExpandTemplate("find {dir} -name {pattern}",
			[]config.CommandArg{{Name: "dir"}, {Name: "pattern"}},
			[]string{"/tmp"},
		)
		if err == nil {
			t.Fatal("expected error for too few values")
		}
		if !strings.Contains(err.Error(), "2 arg(s)") {
			t.Errorf("unexpected error message: %v", err)
		}
	})

	t.Run("too many values", func(t *testing.T) {
		_, err := ExpandTemplate("echo {word}",
			[]config.CommandArg{{Name: "word"}},
			[]string{"hello", "extra"},
		)
		if err == nil {
			t.Fatal("expected error for too many values")
		}
	})

	t.Run("placeholder not in template", func(t *testing.T) {
		_, err := ExpandTemplate("echo hello",
			[]config.CommandArg{{Name: "missing"}},
			[]string{"val"},
		)
		if err == nil {
			t.Fatal("expected error for placeholder not found in template")
		}
		if !strings.Contains(err.Error(), "missing") {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestShellEscape(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"simple", "'simple'"},
		{"", "''"},
		{"it's", "'it'\\''s'"},
		{"a'b'c", "'a'\\''b'\\''c'"},
		{"hello world", "'hello world'"},
		{"$HOME", "'$HOME'"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := ShellEscape(tt.input)
			if got != tt.want {
				t.Errorf("ShellEscape(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
