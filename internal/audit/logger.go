package audit

import (
	"io"
	"time"

	"github.com/rs/zerolog"
)

// Entry represents an audit log entry.
type Entry struct {
	RequestID      string `json:"request_id,omitempty"`
	Endpoint       string `json:"endpoint,omitempty"`
	Method         string `json:"method,omitempty"`
	Path           string `json:"path,omitempty"`
	SourceIP       string `json:"source_ip,omitempty"`
	AgentID        string `json:"agent,omitempty"`
	Decision       string `json:"decision,omitempty"`
	Message        string `json:"message,omitempty"`
	UpstreamStatus int    `json:"upstream_status,omitempty"`
	ResponseBytes  int64  `json:"response_bytes,omitempty"`
	DurationMs     int64  `json:"duration_ms,omitempty"`
}

// Logger writes audit logs.
type Logger struct {
	log         zerolog.Logger
	store       *Store
	storeBodies bool
}

// New creates a new audit logger writing to the given writer.
func New(w io.Writer) *Logger {
	log := zerolog.New(w).With().Logger()
	return &Logger{log: log}
}

// SetStore sets the log store for in-memory storage.
func (l *Logger) SetStore(store *Store) {
	l.store = store
}

// SetStoreBodies enables storing request bodies.
func (l *Logger) SetStoreBodies(enabled bool) {
	l.storeBodies = enabled
}

// Log writes an audit entry.
func (l *Logger) Log(e Entry) {
	l.LogWithBody(e, "")
}

// LogWithBody writes an audit entry with an optional request body.
func (l *Logger) LogWithBody(e Entry, body string) {
	ts := time.Now().UTC()

	event := l.log.Info().
		Str("ts", ts.Format(time.RFC3339)).
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

	// Store in memory if store is configured
	if l.store != nil {
		stored := StoredEntry{
			Entry:     e,
			Timestamp: ts,
		}
		if l.storeBodies && body != "" {
			stored.RequestBody = body
		}
		l.store.Add(stored)
	}
}
