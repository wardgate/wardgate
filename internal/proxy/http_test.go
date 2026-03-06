package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/wardgate/wardgate/internal/auth"
	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/filter"
	"github.com/wardgate/wardgate/internal/grants"
	"github.com/wardgate/wardgate/internal/policy"
	"github.com/wardgate/wardgate/internal/seal"
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

func TestProxy_InjectsBasicAuth(t *testing.T) {
	var receivedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "admin:secret123"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "basic", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	req := httptest.NewRequest("GET", "/data", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	// base64("admin:secret123") = "YWRtaW46c2VjcmV0MTIz"
	expected := "Basic YWRtaW46c2VjcmV0MTIz"
	if receivedAuth != expected {
		t.Errorf("expected %q, got %q", expected, receivedAuth)
	}
}

func TestProxy_InjectsHeaderAuth(t *testing.T) {
	var receivedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "my-access-key"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "header", Header: "Authorization", Prefix: "AccessKey ", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	req := httptest.NewRequest("GET", "/data", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if receivedAuth != "AccessKey my-access-key" {
		t.Errorf("expected 'AccessKey my-access-key', got '%s'", receivedAuth)
	}
}

func TestProxy_InjectsHeaderAuthNoPrefix(t *testing.T) {
	var receivedKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("X-Api-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "raw-key-value"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "header", Header: "X-Api-Key", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	req := httptest.NewRequest("GET", "/data", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if receivedKey != "raw-key-value" {
		t.Errorf("expected 'raw-key-value', got '%s'", receivedKey)
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

func TestProxy_GrantOverridesPolicy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("deleted"))
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "secret-token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	// Static policy denies DELETE
	engine := policy.New([]config.Rule{
		{Match: config.Match{Method: "DELETE"}, Action: "deny", Message: "no deletes"},
		{Match: config.Match{Method: "*"}, Action: "allow"},
	})

	proxy := NewWithName("todoist", endpoint, vault, engine)

	// Add a grant that allows DELETE on endpoint:todoist
	grantStore := grants.NewStore("")
	grantStore.Add(grants.Grant{
		AgentID: "tessa",
		Scope:   "endpoint:todoist",
		Match:   grants.GrantMatch{Method: "DELETE", Path: "/tasks/*"},
		Action:  "allow",
		Reason:  "test grant",
	})
	proxy.SetGrantStore(grantStore)

	req := httptest.NewRequest("DELETE", "/tasks/123", nil)
	req.Header.Set("X-Agent-ID", "tessa")
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	// Should be allowed by grant, not denied by policy
	if rec.Code == http.StatusForbidden {
		t.Error("grant should override static policy deny")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func newTestSealer(t *testing.T) *seal.Sealer {
	t.Helper()
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	s, err := seal.New(hex.EncodeToString(key), 0)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestProxy_SealedHeaderForwarded(t *testing.T) {
	var receivedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	sealer := newTestSealer(t)
	sealedValue, err := sealer.Encrypt("Bearer ghp_realtoken")
	if err != nil {
		t.Fatal(err)
	}

	vault := &mockVault{creds: map[string]string{}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Sealed: true},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	p := New(endpoint, vault, engine)
	p.SetSealer(sealer)
	p.SetAllowedSealHeaders(DefaultAllowedSealHeaders)

	req := httptest.NewRequest("GET", "/repos/owner/repo", nil)
	req.Header.Set("Authorization", "Bearer agent-jwt-token")
	req.Header.Set("X-Wardgate-Sealed-Authorization", sealedValue)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if receivedAuth != "Bearer ghp_realtoken" {
		t.Errorf("expected decrypted token, got %q", receivedAuth)
	}
}

func TestProxy_SealedMultipleHeaders(t *testing.T) {
	var receivedAuth, receivedAPIKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		receivedAPIKey = r.Header.Get("X-Api-Key")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	sealer := newTestSealer(t)
	sealedAuth, _ := sealer.Encrypt("Bearer ghp_token")
	sealedKey, _ := sealer.Encrypt("key_12345")

	vault := &mockVault{creds: map[string]string{}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Sealed: true},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	p := New(endpoint, vault, engine)
	p.SetSealer(sealer)
	p.SetAllowedSealHeaders(DefaultAllowedSealHeaders)

	req := httptest.NewRequest("GET", "/data", nil)
	req.Header.Set("X-Wardgate-Sealed-Authorization", sealedAuth)
	req.Header.Set("X-Wardgate-Sealed-X-Api-Key", sealedKey)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if receivedAuth != "Bearer ghp_token" {
		t.Errorf("expected decrypted auth, got %q", receivedAuth)
	}
	if receivedAPIKey != "key_12345" {
		t.Errorf("expected decrypted API key, got %q", receivedAPIKey)
	}
}

func TestProxy_SealedMissingHeaders(t *testing.T) {
	vault := &mockVault{creds: map[string]string{}}
	endpoint := config.Endpoint{
		Upstream: "http://localhost:1",
		Auth:     config.AuthConfig{Sealed: true},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	sealer := newTestSealer(t)
	p := New(endpoint, vault, engine)
	p.SetSealer(sealer)
	p.SetAllowedSealHeaders(DefaultAllowedSealHeaders)

	req := httptest.NewRequest("GET", "/data", nil)
	// No X-Wardgate-Sealed-* headers
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing sealed headers, got %d", rec.Code)
	}
}

func TestProxy_SealedStripsAgentAuth(t *testing.T) {
	var receivedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	sealer := newTestSealer(t)
	sealedValue, _ := sealer.Encrypt("Bearer upstream-token")

	vault := &mockVault{creds: map[string]string{}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Sealed: true},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	p := New(endpoint, vault, engine)
	p.SetSealer(sealer)
	p.SetAllowedSealHeaders(DefaultAllowedSealHeaders)

	req := httptest.NewRequest("GET", "/data", nil)
	req.Header.Set("Authorization", "Bearer agent-jwt")
	req.Header.Set("X-Wardgate-Sealed-Authorization", sealedValue)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	// Should get the decrypted upstream token, not the agent JWT
	if receivedAuth != "Bearer upstream-token" {
		t.Errorf("expected upstream token, got %q", receivedAuth)
	}
}

func TestProxy_SealedStripsPrefix(t *testing.T) {
	var headers http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		headers = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	sealer := newTestSealer(t)
	sealedValue, _ := sealer.Encrypt("key-value-123")

	vault := &mockVault{creds: map[string]string{}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Sealed: true},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	p := New(endpoint, vault, engine)
	p.SetSealer(sealer)
	p.SetAllowedSealHeaders(DefaultAllowedSealHeaders)

	req := httptest.NewRequest("GET", "/data", nil)
	req.Header.Set("X-Wardgate-Sealed-X-Api-Key", sealedValue)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Sealed header should be stripped
	if headers.Get("X-Wardgate-Sealed-X-Api-Key") != "" {
		t.Error("sealed header should be stripped from upstream request")
	}
	// Decrypted header should be present
	if headers.Get("X-Api-Key") != "key-value-123" {
		t.Errorf("expected decrypted X-Api-Key header, got %q", headers.Get("X-Api-Key"))
	}
}

func TestProxy_SealedInvalidEncryptedValue(t *testing.T) {
	var receivedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	sealer := newTestSealer(t)
	validSealed, _ := sealer.Encrypt("Bearer good-token")

	vault := &mockVault{creds: map[string]string{}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Sealed: true},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	p := New(endpoint, vault, engine)
	p.SetSealer(sealer)
	p.SetAllowedSealHeaders(DefaultAllowedSealHeaders)

	req := httptest.NewRequest("GET", "/data", nil)
	// One valid sealed header, one invalid (tampered)
	req.Header.Set("X-Wardgate-Sealed-Authorization", validSealed)
	req.Header.Set("X-Wardgate-Sealed-X-Api-Key", "not-valid-base64!!!")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Valid sealed header should be decrypted and forwarded
	if receivedHeaders.Get("Authorization") != "Bearer good-token" {
		t.Errorf("expected valid sealed header forwarded, got %q", receivedHeaders.Get("Authorization"))
	}
	// Invalid sealed header should be skipped (not forwarded)
	if receivedHeaders.Get("X-Api-Key") != "" {
		t.Errorf("expected invalid sealed header to be skipped, got %q", receivedHeaders.Get("X-Api-Key"))
	}
}

func TestProxy_SealedPolicyDeny(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called when policy denies")
	}))
	defer upstream.Close()

	sealer := newTestSealer(t)
	sealedValue, _ := sealer.Encrypt("Bearer token")

	vault := &mockVault{creds: map[string]string{}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Sealed: true},
	}
	// Policy denies DELETE
	engine := policy.New([]config.Rule{
		{Match: config.Match{Method: "DELETE"}, Action: "deny", Message: "no deletes"},
	})

	p := New(endpoint, vault, engine)
	p.SetSealer(sealer)
	p.SetAllowedSealHeaders(DefaultAllowedSealHeaders)

	req := httptest.NewRequest("DELETE", "/data/123", nil)
	req.Header.Set("X-Wardgate-Sealed-Authorization", sealedValue)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for policy deny on sealed endpoint, got %d", rec.Code)
	}
}

func TestProxy_SealedResponseBodyForwarded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id": 42, "status": "created"}`))
	}))
	defer upstream.Close()

	sealer := newTestSealer(t)
	sealedValue, _ := sealer.Encrypt("Bearer upstream-token")

	vault := &mockVault{creds: map[string]string{}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Sealed: true},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	p := New(endpoint, vault, engine)
	p.SetSealer(sealer)
	p.SetAllowedSealHeaders(DefaultAllowedSealHeaders)

	req := httptest.NewRequest("POST", "/resources", strings.NewReader(`{"name": "test"}`))
	req.Header.Set("X-Wardgate-Sealed-Authorization", sealedValue)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != `{"id": 42, "status": "created"}` {
		t.Errorf("unexpected response body: %s", body)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Error("Content-Type response header not forwarded")
	}
}

func TestProxy_SealedPassesThroughRegularHeaders(t *testing.T) {
	var receivedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	sealer := newTestSealer(t)
	sealedAuth, _ := sealer.Encrypt("Bearer upstream-token")

	vault := &mockVault{creds: map[string]string{}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Sealed: true},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	p := New(endpoint, vault, engine)
	p.SetSealer(sealer)
	p.SetAllowedSealHeaders(DefaultAllowedSealHeaders)

	req := httptest.NewRequest("POST", "/data", nil)
	req.Header.Set("Authorization", "Bearer agent-jwt")
	req.Header.Set("X-Wardgate-Sealed-Authorization", sealedAuth)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Request-Id", "req-123")
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Regular headers should be forwarded unchanged
	if receivedHeaders.Get("Content-Type") != "application/json" {
		t.Error("Content-Type not forwarded")
	}
	if receivedHeaders.Get("Accept") != "application/json" {
		t.Error("Accept not forwarded")
	}
	if receivedHeaders.Get("X-Request-Id") != "req-123" {
		t.Error("X-Request-Id not forwarded")
	}
	// Sealed header should be replaced with decrypted value
	if receivedHeaders.Get("Authorization") != "Bearer upstream-token" {
		t.Errorf("expected decrypted Authorization, got %q", receivedHeaders.Get("Authorization"))
	}
	// Sealed prefix should be stripped
	if receivedHeaders.Get("X-Wardgate-Sealed-Authorization") != "" {
		t.Error("sealed header prefix should be stripped")
	}
}

func TestProxy_SealedHeaderWhitelistAllowed(t *testing.T) {
	var receivedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	sealer := newTestSealer(t)
	sealedValue, _ := sealer.Encrypt("Bearer ghp_token")

	vault := &mockVault{creds: map[string]string{}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Sealed: true},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	p := New(endpoint, vault, engine)
	p.SetSealer(sealer)
	p.SetAllowedSealHeaders([]string{"Authorization"})

	req := httptest.NewRequest("GET", "/data", nil)
	req.Header.Set("X-Wardgate-Sealed-Authorization", sealedValue)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if receivedAuth != "Bearer ghp_token" {
		t.Errorf("expected decrypted Authorization header, got %q", receivedAuth)
	}
}

func TestProxy_SealedHeaderWhitelistRejected(t *testing.T) {
	var receivedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	sealer := newTestSealer(t)
	sealedHost, _ := sealer.Encrypt("evil.example.com")
	sealedAuth, _ := sealer.Encrypt("Bearer good-token")

	vault := &mockVault{creds: map[string]string{}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Sealed: true},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	p := New(endpoint, vault, engine)
	p.SetSealer(sealer)
	// Only allow Authorization, not Host
	p.SetAllowedSealHeaders([]string{"Authorization"})

	req := httptest.NewRequest("GET", "/data", nil)
	req.Header.Set("X-Wardgate-Sealed-Host", sealedHost)
	req.Header.Set("X-Wardgate-Sealed-Authorization", sealedAuth)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Authorization should be forwarded (it's in the whitelist)
	if receivedHeaders.Get("Authorization") != "Bearer good-token" {
		t.Errorf("expected Authorization forwarded, got %q", receivedHeaders.Get("Authorization"))
	}
	// Sealed Host header should be stripped (not in whitelist)
	if receivedHeaders.Get("X-Wardgate-Sealed-Host") != "" {
		t.Error("sealed Host header should be stripped from upstream request")
	}
}

func TestProxy_SealedHeaderDefaultWhitelist(t *testing.T) {
	var receivedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	sealer := newTestSealer(t)
	sealedAuth, _ := sealer.Encrypt("Bearer token")
	sealedKey, _ := sealer.Encrypt("key_123")

	vault := &mockVault{creds: map[string]string{}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Sealed: true},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	p := New(endpoint, vault, engine)
	p.SetSealer(sealer)
	// Empty list falls back to DefaultAllowedSealHeaders
	p.SetAllowedSealHeaders(nil)

	req := httptest.NewRequest("GET", "/data", nil)
	req.Header.Set("X-Wardgate-Sealed-Authorization", sealedAuth)
	req.Header.Set("X-Wardgate-Sealed-X-Api-Key", sealedKey)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Default whitelist should allow these
	if receivedHeaders.Get("Authorization") != "Bearer token" {
		t.Errorf("expected Authorization from default whitelist, got %q", receivedHeaders.Get("Authorization"))
	}
	if receivedHeaders.Get("X-Api-Key") != "key_123" {
		t.Errorf("expected X-Api-Key from default whitelist, got %q", receivedHeaders.Get("X-Api-Key"))
	}
}

func TestProxy_SealedMultipleValuesForSameHeader(t *testing.T) {
	var receivedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	sealer := newTestSealer(t)
	sealedVal1, _ := sealer.Encrypt("value-one")
	sealedVal2, _ := sealer.Encrypt("value-two")

	vault := &mockVault{creds: map[string]string{}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Sealed: true},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	p := New(endpoint, vault, engine)
	p.SetSealer(sealer)
	p.SetAllowedSealHeaders([]string{"X-Auth-Token"})

	req := httptest.NewRequest("GET", "/data", nil)
	req.Header.Add("X-Wardgate-Sealed-X-Auth-Token", sealedVal1)
	req.Header.Add("X-Wardgate-Sealed-X-Auth-Token", sealedVal2)
	rec := httptest.NewRecorder()

	p.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	// Both decrypted values should be present
	vals := receivedHeaders.Values("X-Auth-Token")
	if len(vals) != 2 {
		t.Fatalf("expected 2 values for X-Auth-Token, got %d: %v", len(vals), vals)
	}
	if vals[0] != "value-one" {
		t.Errorf("expected first value 'value-one', got %q", vals[0])
	}
	if vals[1] != "value-two" {
		t.Errorf("expected second value 'value-two', got %q", vals[1])
	}
	// Sealed prefix should be stripped
	if receivedHeaders.Get("X-Wardgate-Sealed-X-Auth-Token") != "" {
		t.Error("sealed header prefix should be stripped")
	}
}

func TestProxy_DynamicUpstreamAllowed(t *testing.T) {
	var receivedHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		AllowedUpstreams: []string{upstream.URL},
		Auth:             config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("X-Wardgate-Upstream", upstream.URL)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if receivedHost == "" {
		t.Error("expected request to reach upstream")
	}
}

func TestProxy_DynamicUpstreamDenied(t *testing.T) {
	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		AllowedUpstreams: []string{"https://api.github.com"},
		Auth:             config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("X-Wardgate-Upstream", "https://evil.example.com")
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for denied upstream, got %d", rec.Code)
	}
}

func TestProxy_DynamicUpstreamFallsBackToStatic(t *testing.T) {
	var receivedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream:         upstream.URL,
		AllowedUpstreams: []string{"https://other.example.com"},
		Auth:             config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	// No X-Wardgate-Upstream header - should use static upstream
	req := httptest.NewRequest("GET", "/tasks", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for static fallback, got %d", rec.Code)
	}
	if receivedAuth != "Bearer token" {
		t.Errorf("expected bearer token to be injected, got %q", receivedAuth)
	}
}

func TestProxy_DynamicUpstreamHeaderStripped(t *testing.T) {
	var receivedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		AllowedUpstreams: []string{upstream.URL},
		Auth:             config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("X-Wardgate-Upstream", upstream.URL)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if receivedHeaders.Get("X-Wardgate-Upstream") != "" {
		t.Error("X-Wardgate-Upstream header should be stripped before forwarding")
	}
}

func TestProxy_DynamicUpstreamPolicyStillApplies(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called when policy denies")
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		AllowedUpstreams: []string{upstream.URL},
		Auth:             config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	// Policy denies DELETE
	engine := policy.New([]config.Rule{
		{Match: config.Match{Method: "DELETE"}, Action: "deny", Message: "no deletes"},
	})

	proxy := New(endpoint, vault, engine)
	req := httptest.NewRequest("DELETE", "/tasks/123", nil)
	req.Header.Set("X-Wardgate-Upstream", upstream.URL)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for policy deny with dynamic upstream, got %d", rec.Code)
	}
}

func TestProxy_DynamicUpstreamNoHeaderNoStatic(t *testing.T) {
	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		AllowedUpstreams: []string{"https://api.example.com"},
		Auth:             config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	// No X-Wardgate-Upstream header and no static upstream
	req := httptest.NewRequest("GET", "/tasks", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when no upstream available, got %d", rec.Code)
	}
}

func TestProxy_DynamicUpstreamNotConfigured(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	// Sending X-Wardgate-Upstream when no allowed_upstreams configured
	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("X-Wardgate-Upstream", "https://evil.example.com")
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when no allowed_upstreams configured, got %d", rec.Code)
	}
}

func TestProxy_SSEPassthroughNoFilter(t *testing.T) {
	sseData := "data: {\"text\": \"hello\"}\n\ndata: [DONE]\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseData))
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	proxy := New(endpoint, vault, engine)
	req := httptest.NewRequest("GET", "/stream", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for SSE stream, got %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != sseData {
		t.Errorf("expected SSE data to pass through\ngot:  %q\nwant: %q", string(body), sseData)
	}
}

func TestProxy_SSEFilterRedact(t *testing.T) {
	// SSE stream with an API key in data
	sseData := "data: {\"key\": \"sk-1234567890abcdef1234567890abcdef\"}\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseData))
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	f, err := filter.New(filter.Config{
		Enabled:     true,
		Patterns:    []string{"api_keys"},
		Action:      filter.ActionRedact,
		Replacement: "[REDACTED]",
	})
	if err != nil {
		t.Fatalf("failed to create filter: %v", err)
	}

	proxy := New(endpoint, vault, engine)
	proxy.SetFilter(f)

	req := httptest.NewRequest("GET", "/stream", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for SSE redact, got %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	bodyStr := string(body)
	if strings.Contains(bodyStr, "sk-1234567890abcdef1234567890abcdef") {
		t.Error("expected sensitive data to be redacted from SSE stream")
	}
	if !strings.Contains(bodyStr, "[REDACTED]") {
		t.Error("expected redaction marker in SSE output")
	}
}

func TestProxy_SSEFilterBlock(t *testing.T) {
	sseData := "data: {\"key\": \"sk-1234567890abcdef1234567890abcdef\"}\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseData))
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	f, err := filter.New(filter.Config{
		Enabled:  true,
		Patterns: []string{"api_keys"},
		Action:   filter.ActionBlock,
	})
	if err != nil {
		t.Fatalf("failed to create filter: %v", err)
	}

	proxy := New(endpoint, vault, engine)
	proxy.SetFilter(f)

	req := httptest.NewRequest("GET", "/stream", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	body, _ := io.ReadAll(rec.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "event: error") {
		t.Error("expected error event in blocked SSE stream")
	}
	if !strings.Contains(bodyStr, "response blocked") {
		t.Error("expected blocked message in SSE error event")
	}
}

func TestProxy_SSEPassthroughMode(t *testing.T) {
	// SSE with sensitive data should pass through when sse_mode is "passthrough"
	sseData := "data: {\"key\": \"sk-1234567890abcdef1234567890abcdef\"}\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(sseData))
	}))
	defer upstream.Close()

	vault := &mockVault{creds: map[string]string{"TEST_CRED": "token"}}
	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: "TEST_CRED"},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})

	f, err := filter.New(filter.Config{
		Enabled:  true,
		Patterns: []string{"api_keys"},
		Action:   filter.ActionBlock,
		SSEMode:  "passthrough",
	})
	if err != nil {
		t.Fatalf("failed to create filter: %v", err)
	}

	proxy := New(endpoint, vault, engine)
	proxy.SetFilter(f)

	req := httptest.NewRequest("GET", "/stream", nil)
	rec := httptest.NewRecorder()

	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for SSE passthrough mode, got %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Body)
	if string(body) != sseData {
		t.Errorf("expected SSE data to pass through in passthrough mode\ngot:  %q\nwant: %q", string(body), sseData)
	}
}

func TestProxy_NonSSEFilterUnchanged(t *testing.T) {
	// Non-SSE text responses should still be buffered and filtered as before
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

	// Non-SSE should still be blocked by filter
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-SSE blocked content, got %d", rec.Code)
	}
}
