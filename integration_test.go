package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/wardgate/wardgate/internal/audit"
	"github.com/wardgate/wardgate/internal/auth"
	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/policy"
	"github.com/wardgate/wardgate/internal/proxy"
)

func TestIntegration_FullRequestFlow(t *testing.T) {
	// Setup mock upstream
	var receivedAuth string
	var receivedPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"tasks": [{"id": 1, "content": "Test task"}]}`))
	}))
	defer upstream.Close()

	// Setup environment
	os.Setenv("WARDGATE_CRED_TEST_API_KEY", "upstream-secret-key")
	os.Setenv("WARDGATE_AGENT_TEST_KEY", "agent-key-123")
	defer os.Unsetenv("WARDGATE_CRED_TEST_API_KEY")
	defer os.Unsetenv("WARDGATE_AGENT_TEST_KEY")

	// Create config
	cfg := &config.Config{
		Server: config.ServerConfig{Listen: ":8080"},
		Agents: []config.AgentConfig{
			{ID: "test-agent", KeyEnv: "WARDGATE_AGENT_TEST_KEY"},
		},
		Endpoints: map[string]config.Endpoint{
			"test-api": {
				Upstream: upstream.URL,
				Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "WARDGATE_CRED_TEST_API_KEY"},
				Rules: []config.Rule{
					{Match: config.Match{Method: "GET"}, Action: "allow"},
					{Match: config.Match{Method: "DELETE"}, Action: "deny", Message: "Delete not allowed"},
				},
			},
		},
	}

	// Setup components
	vault := auth.NewEnvVault()
	var auditBuf bytes.Buffer
	auditLog := audit.New(&auditBuf)

	// Create handler
	mux := http.NewServeMux()
	for name, endpoint := range cfg.Endpoints {
		engine := policy.New(endpoint.Rules)
		p := proxy.New(endpoint, vault, engine)
		handler := testAuditMiddleware(auditLog, name, p)
		mux.Handle("/"+name+"/", http.StripPrefix("/"+name, handler))
	}

	handler := auth.NewAgentAuthMiddleware(cfg.Agents, mux)

	// Test 1: Allowed request with credential injection
	t.Run("allowed_request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test-api/tasks", nil)
		req.Header.Set("Authorization", "Bearer agent-key-123")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}

		// Verify upstream received correct auth
		if receivedAuth != "Bearer upstream-secret-key" {
			t.Errorf("credential injection failed: got '%s'", receivedAuth)
		}

		// Verify path forwarded correctly
		if receivedPath != "/tasks" {
			t.Errorf("expected path '/tasks', got '%s'", receivedPath)
		}

		// Verify response body
		body, _ := io.ReadAll(rec.Body)
		if !strings.Contains(string(body), "Test task") {
			t.Errorf("unexpected body: %s", body)
		}
	})

	// Test 2: Denied request
	t.Run("denied_request", func(t *testing.T) {
		req := httptest.NewRequest("DELETE", "/test-api/tasks/123", nil)
		req.Header.Set("Authorization", "Bearer agent-key-123")
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", rec.Code)
		}
	})

	// Test 3: Unauthenticated request
	t.Run("unauthenticated_request", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/test-api/tasks", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rec.Code)
		}
	})

	// Test 4: Verify audit log
	t.Run("audit_log", func(t *testing.T) {
		logs := strings.Split(strings.TrimSpace(auditBuf.String()), "\n")
		if len(logs) < 2 {
			t.Fatalf("expected at least 2 audit logs, got %d", len(logs))
		}

		// Check first log entry
		var entry map[string]interface{}
		if err := json.Unmarshal([]byte(logs[0]), &entry); err != nil {
			t.Fatalf("invalid audit log JSON: %v", err)
		}

		if entry["endpoint"] != "test-api" {
			t.Errorf("expected endpoint 'test-api', got %v", entry["endpoint"])
		}
	})
}

func testAuditMiddleware(log *audit.Logger, endpoint string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &testResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)

		log.Log(audit.Entry{
			RequestID: "test-req",
			Endpoint:  endpoint,
			Method:    r.Method,
			Path:      r.URL.Path,
			Decision:  testDecisionFromStatus(rw.status),
		})
	})
}

type testResponseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *testResponseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func testDecisionFromStatus(status int) string {
	if status == http.StatusForbidden {
		return "deny"
	}
	return "allow"
}
