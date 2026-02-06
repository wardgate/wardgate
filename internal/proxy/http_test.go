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
	"github.com/wardgate/wardgate/internal/filter"
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

func TestProxy_FilterBlocksSensitiveData(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Response contains a verification code
		w.Write([]byte(`{"message": "Your verification code is 123456"}`))
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	// Create filter with block action
	f, err := filter.New(filter.Config{
		Enabled:  true,
		Patterns: []string{"otp_codes"},
		Action:   filter.ActionBlock,
	})
	if err != nil {
		t.Fatalf("failed to create filter: %v", err)
	}

	proxy := New(endpoint, vault, engine)
	proxy.SetFilter(f)

	req := httptest.NewRequest("GET", "/verify", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected status 403 when filter blocks, got %d", rec.Code)
	}
}

func TestProxy_FilterRedactsSensitiveData(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message": "Your verification code is 123456"}`))
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	// Create filter with redact action
	f, err := filter.New(filter.Config{
		Enabled:     true,
		Patterns:    []string{"otp_codes"},
		Action:      filter.ActionRedact,
		Replacement: "[REDACTED]",
	})
	if err != nil {
		t.Fatalf("failed to create filter: %v", err)
	}

	proxy := New(endpoint, vault, engine)
	proxy.SetFilter(f)

	req := httptest.NewRequest("GET", "/verify", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 when filter redacts, got %d", rec.Code)
	}

	body, _ := io.ReadAll(rec.Body)
	bodyStr := string(body)

	// Should not contain the original code
	if strings.Contains(bodyStr, "123456") {
		t.Error("response should not contain the original code")
	}
	// Should contain redaction marker
	if !strings.Contains(bodyStr, "[REDACTED]") {
		t.Error("response should contain redaction marker")
	}
}

func TestProxy_FilterDisabledPassesThrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"message": "Your verification code is 123456"}`))
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	// Create disabled filter
	f, err := filter.New(filter.Config{
		Enabled: false,
	})
	if err != nil {
		t.Fatalf("failed to create filter: %v", err)
	}

	proxy := New(endpoint, vault, engine)
	proxy.SetFilter(f)

	req := httptest.NewRequest("GET", "/verify", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	body, _ := io.ReadAll(rec.Body)
	// Should contain the original code when filter is disabled
	if !strings.Contains(string(body), "123456") {
		t.Error("response should contain original code when filter is disabled")
	}
}

func TestProxy_FilterSkipsNonTextContent(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		w.Write([]byte(`Your verification code is 123456`)) // Would be binary in real life
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	// Create filter with block action
	f, err := filter.New(filter.Config{
		Enabled:  true,
		Patterns: []string{"otp_codes"},
		Action:   filter.ActionBlock,
	})
	if err != nil {
		t.Fatalf("failed to create filter: %v", err)
	}

	proxy := New(endpoint, vault, engine)
	proxy.SetFilter(f)

	req := httptest.NewRequest("GET", "/image", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	// Should pass through for non-text content
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for non-text content, got %d", rec.Code)
	}
}
