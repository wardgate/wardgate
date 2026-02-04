package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWebhookChannel_Send(t *testing.T) {
	var received Message
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json")
		}
		if r.Header.Get("X-Custom") != "header" {
			t.Errorf("expected custom header")
		}
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ch := NewWebhookChannel(server.URL, map[string]string{"X-Custom": "header"})

	msg := Message{
		Title:     "Test",
		Body:      "Test body",
		RequestID: "req-123",
		Endpoint:  "test-api",
		Method:    "POST",
		Path:      "/tasks",
		AgentID:   "agent-1",
	}

	err := ch.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	if received.Title != "Test" {
		t.Errorf("expected title 'Test', got '%s'", received.Title)
	}
	if received.RequestID != "req-123" {
		t.Errorf("expected request_id 'req-123', got '%s'", received.RequestID)
	}
}

func TestWebhookChannel_SendError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	ch := NewWebhookChannel(server.URL, nil)

	err := ch.Send(context.Background(), Message{Title: "Test"})
	if err == nil {
		t.Error("expected error for 500 response")
	}
}

func TestSlackChannel_Send(t *testing.T) {
	var received map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ch := NewSlackChannel(server.URL)

	msg := Message{
		Title:        "Approval Required",
		Body:         "Agent wants to POST /tasks",
		RequestID:    "req-123",
		Endpoint:     "test-api",
		Method:       "POST",
		Path:         "/tasks",
		AgentID:      "agent-1",
		DashboardURL: "http://localhost/ui/",
	}

	err := ch.Send(context.Background(), msg)
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Check Slack payload structure
	if received["text"] == nil {
		t.Error("expected text field in Slack payload")
	}
	if received["blocks"] == nil {
		t.Error("expected blocks field in Slack payload")
	}
}
