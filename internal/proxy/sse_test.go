package proxy

import (
	"io"
	"strings"
	"testing"

	"github.com/wardgate/wardgate/internal/filter"
)

// newTestFilter creates a filter for testing SSE functionality.
func newTestFilter(t *testing.T, action filter.Action) *filter.Filter {
	t.Helper()
	f, err := filter.New(filter.Config{
		Enabled:     true,
		Patterns:    []string{"api_keys"},
		Action:      action,
		Replacement: "[REDACTED]",
	})
	if err != nil {
		t.Fatalf("failed to create filter: %v", err)
	}
	return f
}

func TestSSEFilterReader_Passthrough(t *testing.T) {
	input := "event: message\ndata: {\"text\": \"hello world\"}\n\n"
	f := newTestFilter(t, filter.ActionRedact)

	reader := &sseFilterReader{
		reader: io.NopCloser(strings.NewReader(input)),
		filter: f,
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if string(output) != input {
		t.Errorf("expected passthrough for clean data\ngot:  %q\nwant: %q", string(output), input)
	}
}

func TestSSEFilterReader_RedactSensitiveData(t *testing.T) {
	// api_keys pattern should match "sk-..." style keys
	input := "data: {\"key\": \"sk-1234567890abcdef1234567890abcdef\"}\n\n"
	f := newTestFilter(t, filter.ActionRedact)

	reader := &sseFilterReader{
		reader: io.NopCloser(strings.NewReader(input)),
		filter: f,
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := string(output)
	if strings.Contains(result, "sk-1234567890abcdef1234567890abcdef") {
		t.Error("expected sensitive data to be redacted")
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Error("expected redaction marker in output")
	}
}

func TestSSEFilterReader_BlockSensitiveData(t *testing.T) {
	input := "data: {\"key\": \"sk-1234567890abcdef1234567890abcdef\"}\n\n"
	f := newTestFilter(t, filter.ActionBlock)

	reader := &sseFilterReader{
		reader: io.NopCloser(strings.NewReader(input)),
		filter: f,
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := string(output)
	if !strings.Contains(result, "event: error") {
		t.Error("expected error event on block")
	}
	if !strings.Contains(result, "response blocked") {
		t.Error("expected blocked message in error event")
	}
}

func TestSSEFilterReader_MetadataPassthrough(t *testing.T) {
	input := "id: 123\nevent: message\nretry: 5000\ndata: {\"text\": \"hello\"}\n\n"
	f := newTestFilter(t, filter.ActionRedact)

	reader := &sseFilterReader{
		reader: io.NopCloser(strings.NewReader(input)),
		filter: f,
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := string(output)
	// Metadata lines should pass through unchanged
	if !strings.Contains(result, "id: 123") {
		t.Error("expected id line to pass through")
	}
	if !strings.Contains(result, "event: message") {
		t.Error("expected event line to pass through")
	}
	if !strings.Contains(result, "retry: 5000") {
		t.Error("expected retry line to pass through")
	}
}

func TestSSEFilterReader_DoneSentinel(t *testing.T) {
	input := "data: [DONE]\n\n"
	f := newTestFilter(t, filter.ActionBlock)

	reader := &sseFilterReader{
		reader: io.NopCloser(strings.NewReader(input)),
		filter: f,
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// [DONE] should pass through even with block action
	if string(output) != input {
		t.Errorf("expected [DONE] to pass through unchanged\ngot:  %q\nwant: %q", string(output), input)
	}
}

func TestSSEFilterReader_MultipleMessages(t *testing.T) {
	input := "data: {\"text\": \"hello\"}\n\ndata: {\"text\": \"world\"}\n\ndata: [DONE]\n\n"
	f := newTestFilter(t, filter.ActionRedact)

	reader := &sseFilterReader{
		reader: io.NopCloser(strings.NewReader(input)),
		filter: f,
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All messages should pass through (no sensitive data)
	if string(output) != input {
		t.Errorf("expected clean messages to pass through\ngot:  %q\nwant: %q", string(output), input)
	}
}

func TestSSEFilterReader_CommentLines(t *testing.T) {
	input := ": this is a comment\ndata: {\"text\": \"hello\"}\n\n"
	f := newTestFilter(t, filter.ActionRedact)

	reader := &sseFilterReader{
		reader: io.NopCloser(strings.NewReader(input)),
		filter: f,
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(string(output), ": this is a comment") {
		t.Error("expected comment line to pass through")
	}
}

func TestSSEFilterReader_MultiLineData(t *testing.T) {
	// Multiple data: lines in one message are concatenated with newlines
	input := "data: {\"text\":\n" +
		"data:  \"hello world\"}\n\n"
	f := newTestFilter(t, filter.ActionRedact)

	reader := &sseFilterReader{
		reader: io.NopCloser(strings.NewReader(input)),
		filter: f,
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := string(output)
	// Should contain both data lines
	if !strings.Contains(result, "data:") {
		t.Error("expected data lines in output")
	}
}

func TestSSEFilterReader_OpenAIFormat(t *testing.T) {
	// Simulate typical OpenAI SSE format
	input := `data: {"id":"chatcmpl-123","object":"chat.completion.chunk","choices":[{"delta":{"content":"Hello"}}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","choices":[{"delta":{"content":" World"}}]}

data: [DONE]

`
	f := newTestFilter(t, filter.ActionRedact)

	reader := &sseFilterReader{
		reader: io.NopCloser(strings.NewReader(input)),
		filter: f,
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Clean data should pass through unchanged
	if string(output) != input {
		t.Errorf("expected OpenAI format to pass through\ngot:  %q\nwant: %q", string(output), input)
	}
}

func TestSSEFilterReader_EmptyStream(t *testing.T) {
	reader := &sseFilterReader{
		reader: io.NopCloser(strings.NewReader("")),
		filter: newTestFilter(t, filter.ActionRedact),
	}

	output, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(output) != 0 {
		t.Errorf("expected empty output for empty stream, got %q", string(output))
	}
}
