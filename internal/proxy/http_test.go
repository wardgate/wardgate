package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wardgate/wardgate/internal/auth"
	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/policy"
)

// mockVault implements auth.Vault for testing
type mockVault struct {
	creds map[string]string
}

func (m *mockVault) Get(name string) (string, error) {
	if cred, ok := m.creds[name]; ok {
		return cred, nil
	}
	return "", auth.ErrCredentialNotFound
}

func TestProxy_InjectsBearerToken(t *testing.T) {
	var receivedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "secret-token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	req := httptest.NewRequest("GET", "/tasks", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if receivedAuth != "Bearer secret-token" {
		t.Errorf("expected 'Bearer secret-token', got '%s'", receivedAuth)
	}
}

func TestProxy_PreservesOriginalHeaders(t *testing.T) {
	var receivedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("X-Custom-Header", "custom-value")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if receivedHeaders.Get("X-Custom-Header") != "custom-value" {
		t.Error("custom header not preserved")
	}
	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Error("content-type not preserved")
	}
}

func TestProxy_ForwardsResponseStatusAndBody(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Response-Header", "response-value")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 123}`))
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	req := httptest.NewRequest("POST", "/tasks", strings.NewReader(`{"content": "test"}`))
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != `{"id": 123}` {
		t.Errorf("unexpected body: %s", body)
	}
	if rec.Header().Get("X-Response-Header") != "response-value" {
		t.Error("response header not forwarded")
	}
}

func TestProxy_HandlesUpstreamTimeout(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	proxy.SetTimeout(100 * time.Millisecond)

	req := httptest.NewRequest("GET", "/slow", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Errorf("expected status 504, got %d", rec.Code)
	}
}

func TestProxy_HandlesUpstreamConnectionError(t *testing.T) {
	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream: "http://localhost:59999", // non-existent server
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	proxy.SetTimeout(500 * time.Millisecond)

	req := httptest.NewRequest("GET", "/tasks", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected status 502, got %d", rec.Code)
	}
}

func TestProxy_StripsAgentAuthHeader(t *testing.T) {
	var receivedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "upstream-token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer agent-token") // agent's auth
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	// Should have upstream token, not agent token
	if receivedAuth != "Bearer upstream-token" {
		t.Errorf("expected upstream token, got '%s'", receivedAuth)
	}
}

func TestProxy_PolicyDenyReturns403(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called when policy denies")
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{
		{Match: config.Match{Method: "DELETE"}, Action: "deny", Message: "Deletion not allowed"},
	})

	proxy := New(endpoint, vault, engine)
	req := httptest.NewRequest("DELETE", "/tasks/123", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403, got %d", rec.Code)
	}
}
