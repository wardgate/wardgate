package audit

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"
)

func TestLogger_OutputsValidJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)

	logger.Log(Entry{
		RequestID: "req-123",
		Endpoint:  "todoist-api",
		Method:    "GET",
		Path:      "/tasks",
		Decision:  "allow",
	})

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v\nOutput: %s", err, buf.String())
	}
}

func TestLogger_ContainsRequiredFields(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)

	logger.Log(Entry{
		RequestID:      "req-456",
		Endpoint:       "todoist-api",
		Method:         "POST",
		Path:           "/tasks",
		Decision:       "allow",
		UpstreamStatus: 201,
		DurationMs:     42,
	})

	var result map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}

	required := []string{"ts", "request_id", "endpoint", "method", "path", "decision", "duration_ms"}
	for _, field := range required {
		if _, ok := result[field]; !ok {
			t.Errorf("missing required field: %s", field)
		}
	}
}

func TestLogger_LogsAllowedRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)

	logger.Log(Entry{
		RequestID:      "req-allow",
		Endpoint:       "todoist-api",
		Method:         "GET",
		Path:           "/tasks",
		Decision:       "allow",
		UpstreamStatus: 200,
		DurationMs:     15,
	})

	var result map[string]interface{}
	json.Unmarshal(buf.Bytes(), &result)

	if result["decision"] != "allow" {
		t.Errorf("expected decision 'allow', got %v", result["decision"])
	}
	if result["upstream_status"].(float64) != 200 {
		t.Errorf("expected upstream_status 200, got %v", result["upstream_status"])
	}
}

func TestLogger_LogsDeniedRequest(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)

	logger.Log(Entry{
		RequestID: "req-deny",
		Endpoint:  "todoist-api",
		Method:    "DELETE",
		Path:      "/tasks/123",
		Decision:  "deny",
		Message:   "Deletion not allowed",
	})

	var result map[string]interface{}
	json.Unmarshal(buf.Bytes(), &result)

	if result["decision"] != "deny" {
		t.Errorf("expected decision 'deny', got %v", result["decision"])
	}
	if result["message"] != "Deletion not allowed" {
		t.Errorf("expected message, got %v", result["message"])
	}
}

func TestLogger_TimestampFormat(t *testing.T) {
	var buf bytes.Buffer
	logger := New(&buf)

	logger.Log(Entry{
		RequestID: "req-ts",
		Endpoint:  "test",
		Method:    "GET",
		Path:      "/",
		Decision:  "allow",
	})

	var result map[string]interface{}
	json.Unmarshal(buf.Bytes(), &result)

	ts, ok := result["ts"].(string)
	if !ok {
		t.Fatal("ts field not a string")
	}

	// Verify it's a valid RFC3339 timestamp
	_, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Errorf("invalid timestamp format: %v", err)
	}
}
