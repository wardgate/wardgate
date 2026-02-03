package audit

import (
	"io"
	"time"

	"github.com/rs/zerolog"
)

// Entry represents an audit log entry.
type Entry struct {
	RequestID      string
	Endpoint       string
	Method         string
	Path           string
	SourceIP       string
	AgentID        string
	Decision       string
	Message        string
	UpstreamStatus int
	ResponseBytes  int64
	DurationMs     int64
}

// Logger writes audit logs.
type Logger struct {
	log zerolog.Logger
}

// New creates a new audit logger writing to the given writer.
func New(w io.Writer) *Logger {
	log := zerolog.New(w).With().Logger()
	return &Logger{log: log}
}

// Log writes an audit entry.
func (l *Logger) Log(e Entry) {
	event := l.log.Info().
		Str("ts", time.Now().UTC().Format(time.RFC3339)).
		Str("request_id", e.RequestID).
		Str("endpoint", e.Endpoint).
		Str("method", e.Method).
		Str("path", e.Path).
		Str("decision", e.Decision).
		Int64("duration_ms", e.DurationMs)

	if e.SourceIP != "" {
		event = event.Str("source_ip", e.SourceIP)
	}
	if e.AgentID != "" {
		event = event.Str("agent", e.AgentID)
	}
	if e.Message != "" {
		event = event.Str("message", e.Message)
	}
	if e.UpstreamStatus != 0 {
		event = event.Int("upstream_status", e.UpstreamStatus)
	}
	if e.ResponseBytes != 0 {
		event = event.Int64("response_bytes", e.ResponseBytes)
	}

	event.Send()
}
