package auth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/wardgate/wardgate/internal/config"
)

func TestAgentAuth_ValidToken(t *testing.T) {
	os.Setenv("WARDGATE_AGENT_TESSA_KEY", "valid-agent-key")
	defer os.Unsetenv("WARDGATE_AGENT_TESSA_KEY")

	agents := []config.AgentConfig{
		{ID: "tessa", KeyEnv: "WARDGATE_AGENT_TESSA_KEY"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	middleware := NewAgentAuthMiddleware(agents, nil, handler)

	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer valid-agent-key")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestAgentAuth_InvalidToken(t *testing.T) {
	os.Setenv("WARDGATE_AGENT_TESSA_KEY", "valid-agent-key")
	defer os.Unsetenv("WARDGATE_AGENT_TESSA_KEY")

	agents := []config.AgentConfig{
		{ID: "tessa", KeyEnv: "WARDGATE_AGENT_TESSA_KEY"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for invalid token")
	})

	middleware := NewAgentAuthMiddleware(agents, nil, handler)

	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAgentAuth_MissingHeader(t *testing.T) {
	agents := []config.AgentConfig{
		{ID: "tessa", KeyEnv: "WARDGATE_AGENT_TESSA_KEY"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for missing header")
	})

	middleware := NewAgentAuthMiddleware(agents, nil, handler)

	req := httptest.NewRequest("GET", "/tasks", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAgentAuth_MalformedHeader(t *testing.T) {
	agents := []config.AgentConfig{
		{ID: "tessa", KeyEnv: "WARDGATE_AGENT_TESSA_KEY"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for malformed header")
	})

	middleware := NewAgentAuthMiddleware(agents, nil, handler)

	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "NotBearer some-token")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestAgentAuth_MultipleAgents(t *testing.T) {
	os.Setenv("WARDGATE_AGENT_TESSA_KEY", "tessa-key")
	os.Setenv("WARDGATE_AGENT_BOT_KEY", "bot-key")
	defer os.Unsetenv("WARDGATE_AGENT_TESSA_KEY")
	defer os.Unsetenv("WARDGATE_AGENT_BOT_KEY")

	agents := []config.AgentConfig{
		{ID: "tessa", KeyEnv: "WARDGATE_AGENT_TESSA_KEY"},
		{ID: "bot", KeyEnv: "WARDGATE_AGENT_BOT_KEY"},
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAgentAuthMiddleware(agents, nil, handler)

	// Test both agents
	for _, key := range []string{"tessa-key", "bot-key"} {
		req := httptest.NewRequest("GET", "/tasks", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("agent key %s: expected 200, got %d", key, rec.Code)
		}
	}
}
