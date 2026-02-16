package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/wardgate/wardgate/internal/config"
)

// Tests for agent auth middleware based on Phase 2 spec:
// - Middleware should set X-Agent-ID header after successful auth
// - X-Agent-ID should match the agent's configured ID

func TestMiddleware_SetsAgentID(t *testing.T) {
	os.Setenv("TEST_AGENT_KEY", "secret-key-123")
	defer os.Unsetenv("TEST_AGENT_KEY")

	agents := []config.AgentConfig{
		{ID: "tessa", KeyEnv: "TEST_AGENT_KEY"},
	}

	var receivedAgentID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAgentID = r.Header.Get("X-Agent-ID")
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAgentAuthMiddleware(agents, nil, next)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer secret-key-123")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	if receivedAgentID != "tessa" {
		t.Errorf("expected X-Agent-ID 'tessa', got '%s'", receivedAgentID)
	}
}

func TestMiddleware_DifferentAgents(t *testing.T) {
	os.Setenv("AGENT_1_KEY", "key-one")
	os.Setenv("AGENT_2_KEY", "key-two")
	defer os.Unsetenv("AGENT_1_KEY")
	defer os.Unsetenv("AGENT_2_KEY")

	agents := []config.AgentConfig{
		{ID: "agent-alpha", KeyEnv: "AGENT_1_KEY"},
		{ID: "agent-beta", KeyEnv: "AGENT_2_KEY"},
	}

	var receivedAgentID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAgentID = r.Header.Get("X-Agent-ID")
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAgentAuthMiddleware(agents, nil, next)

	tests := []struct {
		key        string
		expectedID string
	}{
		{"key-one", "agent-alpha"},
		{"key-two", "agent-beta"},
	}

	for _, tt := range tests {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("Authorization", "Bearer "+tt.key)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if receivedAgentID != tt.expectedID {
			t.Errorf("key %s: expected X-Agent-ID '%s', got '%s'", tt.key, tt.expectedID, receivedAgentID)
		}
	}
}

func TestMiddleware_NoAgentID_WhenUnauthorized(t *testing.T) {
	os.Setenv("TEST_AGENT_KEY", "secret-key")
	defer os.Unsetenv("TEST_AGENT_KEY")

	agents := []config.AgentConfig{
		{ID: "tessa", KeyEnv: "TEST_AGENT_KEY"},
	}

	nextCalled := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	})

	middleware := NewAgentAuthMiddleware(agents, nil, next)

	// Wrong key
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
	if nextCalled {
		t.Error("next handler should not be called on auth failure")
	}
}
