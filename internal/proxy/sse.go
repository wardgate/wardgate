package proxy

import (
	"bufio"
	"bytes"
	"io"
	"strings"

	"github.com/wardgate/wardgate/internal/filter"
)

// sseFilterReader wraps an SSE stream body and filters each SSE message
// through the sensitive data filter individually.
type sseFilterReader struct {
	reader  io.ReadCloser
	filter  *filter.Filter
	scanner *bufio.Scanner
	buf     bytes.Buffer
	done    bool
}

func (r *sseFilterReader) Read(p []byte) (int, error) {
	if r.buf.Len() > 0 {
		return r.buf.Read(p)
	}

	if r.done {
		return 0, io.EOF
	}

	if r.scanner == nil {
		r.scanner = bufio.NewScanner(r.reader)
		r.scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024) // 1MB max line
	}

	var lines []string
	gotMessage := false

	for r.scanner.Scan() {
		line := r.scanner.Text()
		lines = append(lines, line)

		if line == "" {
			gotMessage = true
			break
		}
	}

	if err := r.scanner.Err(); err != nil {
		r.done = true
		r.buf.WriteString("event: error\ndata: {\"error\":\"stream line too long\"}\n\n")
		return r.buf.Read(p)
	}

	if !gotMessage && len(lines) == 0 {
		r.done = true
		return 0, io.EOF
	}

	// Process the SSE message through the filter
	filtered := r.filterSSEMessage(lines)
	r.buf.WriteString(filtered)

	if !gotMessage {
		// End of stream reached mid-message
		r.done = true
	}

	return r.buf.Read(p)
}

func (r *sseFilterReader) Close() error {
	return r.reader.Close()
}

// filterSSEMessage filters a single SSE message (slice of lines including the trailing empty line).
// Only data: fields are scanned/filtered; metadata lines pass through unchanged.
func (r *sseFilterReader) filterSSEMessage(lines []string) string {
	// Extract data content for scanning
	var dataContent strings.Builder
	hasData := false

	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			hasData = true
			data := strings.TrimPrefix(line, "data:")
			data = strings.TrimPrefix(data, " ") // Optional space after "data:"
			if dataContent.Len() > 0 {
				dataContent.WriteString("\n")
			}
			dataContent.WriteString(data)
		}
	}

	if !hasData {
		// No data fields - pass through unchanged (comments, empty messages, etc.)
		return joinLines(lines)
	}

	content := dataContent.String()

	// Don't filter the [DONE] sentinel
	if content == "[DONE]" {
		return joinLines(lines)
	}

	// Scan for sensitive data
	matches := r.filter.Scan(content)
	if len(matches) == 0 {
		// No sensitive data - pass through unchanged
		return joinLines(lines)
	}

	// Handle based on filter action
	if r.filter.ShouldBlock(matches) {
		// Emit generic error event and signal EOF.
		// Do not include pattern names — they help attackers craft bypasses.
		r.done = true
		return "event: error\ndata: {\"error\":\"response blocked\"}\n\n"
	}

	// Redact: apply filter to data fields, keep metadata lines unchanged
	filtered := r.filter.Apply(content, matches)
	filteredParts := strings.Split(filtered, "\n")

	var result strings.Builder
	dataIdx := 0
	for _, line := range lines {
		if strings.HasPrefix(line, "data:") {
			space := " "
			if len(line) > 5 && line[5] != ' ' {
				space = ""
			}
			if dataIdx < len(filteredParts) {
				result.WriteString("data:" + space + filteredParts[dataIdx] + "\n")
				dataIdx++
			}
		} else {
			result.WriteString(line + "\n")
		}
	}

	return result.String()
}

func joinLines(lines []string) string {
	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
