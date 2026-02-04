package proxy

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/wardgate/wardgate/internal/approval"
	"github.com/wardgate/wardgate/internal/auth"
	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/policy"
)

// Tests for proxy handling all policy actions based on Phase 2 spec:
// - allow: forward to upstream
// - deny: return 403
// - ask: require approval, block until approved/denied/timeout
// - rate_limited: return 429

func TestProxy_AllowAction(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("upstream response"))
	}))
	defer upstream.Close()

	os.Setenv("TEST_CRED", "upstream-secret")
	defer os.Unsetenv("TEST_CRED")

	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
		Rules: []config.Rule{
			{Match: config.Match{Method: "GET"}, Action: "allow"},
		},
	}

	vault := auth.NewEnvVault()
	engine := policy.New(endpoint.Rules)
	p := New(endpoint, vault, engine)

	req := httptest.NewRequest("GET", "/tasks", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for allow, got %d", rec.Code)
	}
}

func TestProxy_DenyAction(t *testing.T) {
	os.Setenv("TEST_CRED", "secret")
	defer os.Unsetenv("TEST_CRED")

	endpoint := config.Endpoint{
		Upstream: "http://example.com",
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
		Rules: []config.Rule{
			{Match: config.Match{Method: "DELETE"}, Action: "deny", Message: "no deletes"},
		},
	}

	vault := auth.NewEnvVault()
	engine := policy.New(endpoint.Rules)
	p := New(endpoint, vault, engine)

	req := httptest.NewRequest("DELETE", "/tasks/123", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for deny, got %d", rec.Code)
	}
}

func TestProxy_RateLimitedAction(t *testing.T) {
	os.Setenv("TEST_CRED", "secret")
	defer os.Unsetenv("TEST_CRED")

	endpoint := config.Endpoint{
		Upstream: "http://example.com",
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
		Rules: []config.Rule{
			{
				Match:     config.Match{Method: "GET"},
				Action:    "allow",
				RateLimit: &config.RateLimit{Max: 2, Window: "1m"},
			},
		},
	}

	vault := auth.NewEnvVault()
	engine := policy.New(endpoint.Rules)
	p := New(endpoint, vault, engine)

	// Make requests until rate limited
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/tasks", nil)
		req.Header.Set("X-Agent-ID", "test-agent")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)

		if i < 2 {
			// First 2 should work (or at least not be 429)
			// Note: without upstream they may fail differently
		} else {
			// 3rd should be rate limited
			if rec.Code != http.StatusTooManyRequests {
				t.Errorf("request %d: expected 429, got %d", i+1, rec.Code)
			}
		}
	}
}

func TestProxy_AskAction_NoApprovalManager(t *testing.T) {
	os.Setenv("TEST_CRED", "secret")
	defer os.Unsetenv("TEST_CRED")

	endpoint := config.Endpoint{
		Upstream: "http://example.com",
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
		Rules: []config.Rule{
			{Match: config.Match{Method: "PUT"}, Action: "ask"},
		},
	}

	vault := auth.NewEnvVault()
	engine := policy.New(endpoint.Rules)
	p := New(endpoint, vault, engine)
	// Note: no approval manager set

	req := httptest.NewRequest("PUT", "/tasks/123", nil)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	// Should fail gracefully when no approval manager
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for ask without approval manager, got %d", rec.Code)
	}
}

func TestProxy_AskAction_Approved(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	os.Setenv("TEST_CRED", "secret")
	defer os.Unsetenv("TEST_CRED")

	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
		Rules: []config.Rule{
			{Match: config.Match{Method: "PUT"}, Action: "ask"},
		},
	}

	vault := auth.NewEnvVault()
	engine := policy.New(endpoint.Rules)
	p := NewWithName("test-api", endpoint, vault, engine)

	// Create approval manager with short timeout
	approvalMgr := approval.NewManager("http://localhost", 5*time.Second)
	p.SetApprovalManager(approvalMgr)

	// Start request in goroutine (it will block waiting for approval)
	done := make(chan int)
	go func() {
		req := httptest.NewRequest("PUT", "/tasks/123", nil)
		req.Header.Set("X-Agent-ID", "test-agent")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		done <- rec.Code
	}()

	// Wait for approval request to be created
	time.Sleep(100 * time.Millisecond)

	// Find and approve the request
	reqID, found := approvalMgr.GetPending()
	if !found {
		t.Fatal("no pending approval request found")
	}

	if err := approvalMgr.ApproveByID(reqID); err != nil {
		t.Fatalf("failed to approve: %v", err)
	}

	// Wait for result
	select {
	case code := <-done:
		if code != http.StatusOK {
			t.Errorf("expected 200 after approval, got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request did not complete after approval")
	}
}

func TestProxy_AskAction_Denied(t *testing.T) {
	os.Setenv("TEST_CRED", "secret")
	defer os.Unsetenv("TEST_CRED")

	endpoint := config.Endpoint{
		Upstream: "http://example.com",
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
		Rules: []config.Rule{
			{Match: config.Match{Method: "DELETE"}, Action: "ask"},
		},
	}

	vault := auth.NewEnvVault()
	engine := policy.New(endpoint.Rules)
	p := NewWithName("test-api", endpoint, vault, engine)

	approvalMgr := approval.NewManager("http://localhost", 5*time.Second)
	p.SetApprovalManager(approvalMgr)

	done := make(chan int)
	go func() {
		req := httptest.NewRequest("DELETE", "/tasks/123", nil)
		req.Header.Set("X-Agent-ID", "test-agent")
		rec := httptest.NewRecorder()
		p.ServeHTTP(rec, req)
		done <- rec.Code
	}()

	time.Sleep(100 * time.Millisecond)

	reqID, found := approvalMgr.GetPending()
	if !found {
		t.Fatal("no pending approval request found")
	}

	if err := approvalMgr.DenyByID(reqID); err != nil {
		t.Fatalf("failed to deny: %v", err)
	}

	select {
	case code := <-done:
		if code != http.StatusForbidden {
			t.Errorf("expected 403 after denial, got %d", code)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request did not complete after denial")
	}
}

func TestProxy_AskAction_Timeout(t *testing.T) {
	os.Setenv("TEST_CRED", "secret")
	defer os.Unsetenv("TEST_CRED")

	endpoint := config.Endpoint{
		Upstream: "http://example.com",
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
		Rules: []config.Rule{
			{Match: config.Match{Method: "PUT"}, Action: "ask"},
		},
	}

	vault := auth.NewEnvVault()
	engine := policy.New(endpoint.Rules)
	p := NewWithName("test-api", endpoint, vault, engine)

	// Very short timeout
	approvalMgr := approval.NewManager("http://localhost", 100*time.Millisecond)
	p.SetApprovalManager(approvalMgr)

	req := httptest.NewRequest("PUT", "/tasks/123", nil)
	req.Header.Set("X-Agent-ID", "test-agent")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	// Should timeout and return 403
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 on timeout, got %d", rec.Code)
	}
}
