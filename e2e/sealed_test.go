package e2e

import (
	"crypto/rand"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wardgate/wardgate/internal/auth"
	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/policy"
	"github.com/wardgate/wardgate/internal/proxy"
	"github.com/wardgate/wardgate/internal/seal"
)

// TestSealedCredentials_E2E exercises the full sealed credentials flow:
//
//	agent → auth middleware → policy engine → proxy (sealed header decryption) → upstream
//
// This tests the same code path as a real Wardgate deployment, wired together
// the same way main.go does it, but using httptest servers.
func TestSealedCredentials_E2E(t *testing.T) {
	// --- 1. Generate keys ---
	sealKey := randomHexKey(t, 32)
	agentKey := "test-agent-key-" + randomHexKey(t, 8)

	// --- 2. Create sealer ---
	sealer, err := seal.New(sealKey, 100)
	if err != nil {
		t.Fatalf("seal.New: %v", err)
	}

	// --- 3. Encrypt upstream credentials ---
	sealedAuth, err := sealer.Encrypt("Bearer ghp_realtoken123")
	if err != nil {
		t.Fatalf("Encrypt auth: %v", err)
	}
	sealedAPIKey, err := sealer.Encrypt("sk-secret-api-key")
	if err != nil {
		t.Fatalf("Encrypt API key: %v", err)
	}

	// --- 4. Start mock upstream server ---
	type upstreamRequest struct {
		method  string
		path    string
		headers http.Header
		body    string
	}
	var lastUpstreamReq upstreamRequest

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		lastUpstreamReq = upstreamRequest{
			method:  r.Method,
			path:    r.URL.Path,
			headers: r.Header.Clone(),
			body:    string(body),
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Upstream-Response", "true")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"result": "success", "items": [1, 2, 3]}`))
	}))
	defer upstream.Close()

	// --- 5. Build Wardgate config ---
	agentKeyEnv := "TEST_E2E_AGENT_KEY"
	t.Setenv(agentKeyEnv, agentKey)

	agents := []config.AgentConfig{
		{ID: "test-agent", KeyEnv: agentKeyEnv},
	}

	endpoint := config.Endpoint{
		Upstream: upstream.URL,
		Auth:     config.AuthConfig{Sealed: true},
	}

	rules := []config.Rule{
		{Match: config.Match{Method: "GET"}, Action: "allow"},
		{Match: config.Match{Method: "POST", Path: "/resources"}, Action: "allow"},
		{Match: config.Match{Method: "DELETE"}, Action: "deny", Message: "deletion not allowed"},
		{Match: config.Match{Method: "*"}, Action: "deny", Message: "not permitted"},
	}
	engine := policy.New(rules)

	// --- 6. Wire up components (mirrors main.go) ---
	vault := &mockVault{creds: map[string]string{}}
	p := proxy.NewWithName("sealed-api", endpoint, vault, engine)
	p.SetSealer(sealer)
	p.SetAllowedSealHeaders([]string{
		"Authorization",
		"X-Api-Key",
		"X-Bad-Header",
		"X-Custom-Header",
	})

	apiMux := http.NewServeMux()
	apiMux.Handle("/sealed-api/", http.StripPrefix("/sealed-api", p))

	// Wrap with agent auth middleware (same as main.go)
	gateway := auth.NewAgentAuthMiddleware(agents, nil, apiMux)

	// --- 7. Start gateway server ---
	gw := httptest.NewServer(gateway)
	defer gw.Close()

	client := gw.Client()

	// ==========================================
	// Test cases
	// ==========================================

	t.Run("unauthenticated request returns 401", func(t *testing.T) {
		req, _ := http.NewRequest("GET", gw.URL+"/sealed-api/data", nil)
		// No Authorization header
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("wrong agent key returns 401", func(t *testing.T) {
		req, _ := http.NewRequest("GET", gw.URL+"/sealed-api/data", nil)
		req.Header.Set("Authorization", "Bearer wrong-key")
		req.Header.Set("X-Wardgate-Sealed-Authorization", sealedAuth)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("missing sealed headers returns 400", func(t *testing.T) {
		req, _ := http.NewRequest("GET", gw.URL+"/sealed-api/data", nil)
		req.Header.Set("Authorization", "Bearer "+agentKey)
		// No X-Wardgate-Sealed-* headers
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("policy deny returns 403", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", gw.URL+"/sealed-api/resources/123", nil)
		req.Header.Set("Authorization", "Bearer "+agentKey)
		req.Header.Set("X-Wardgate-Sealed-Authorization", sealedAuth)
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("sealed GET: decrypts and forwards single header", func(t *testing.T) {
		req, _ := http.NewRequest("GET", gw.URL+"/sealed-api/repos/owner/repo", nil)
		req.Header.Set("Authorization", "Bearer "+agentKey)
		req.Header.Set("X-Wardgate-Sealed-Authorization", sealedAuth)
		req.Header.Set("Accept", "application/json")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		// Verify upstream received decrypted Authorization
		if got := lastUpstreamReq.headers.Get("Authorization"); got != "Bearer ghp_realtoken123" {
			t.Errorf("upstream Authorization = %q, want %q", got, "Bearer ghp_realtoken123")
		}

		// Verify agent's original auth was stripped (not forwarded to upstream)
		// The decrypted value should be there, not the agent's JWT
		if got := lastUpstreamReq.headers.Get("Authorization"); strings.Contains(got, agentKey) {
			t.Error("agent auth key leaked to upstream")
		}

		// Verify sealed header prefix was stripped
		if got := lastUpstreamReq.headers.Get("X-Wardgate-Sealed-Authorization"); got != "" {
			t.Errorf("sealed header prefix leaked to upstream: %q", got)
		}

		// Verify regular headers passed through
		if got := lastUpstreamReq.headers.Get("Accept"); got != "application/json" {
			t.Errorf("Accept header not forwarded: %q", got)
		}

		// Verify response from upstream forwarded back
		body, _ := io.ReadAll(resp.Body)
		if !strings.Contains(string(body), `"result": "success"`) {
			t.Errorf("unexpected response body: %s", body)
		}
		if resp.Header.Get("X-Upstream-Response") != "true" {
			t.Error("upstream response headers not forwarded")
		}
	})

	t.Run("sealed POST: decrypts multiple headers and forwards body", func(t *testing.T) {
		reqBody := `{"name": "test-resource"}`
		req, _ := http.NewRequest("POST", gw.URL+"/sealed-api/resources", strings.NewReader(reqBody))
		req.Header.Set("Authorization", "Bearer "+agentKey)
		req.Header.Set("X-Wardgate-Sealed-Authorization", sealedAuth)
		req.Header.Set("X-Wardgate-Sealed-X-Api-Key", sealedAPIKey)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Request-Id", "req-e2e-001")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		// Verify both sealed headers decrypted
		if got := lastUpstreamReq.headers.Get("Authorization"); got != "Bearer ghp_realtoken123" {
			t.Errorf("upstream Authorization = %q, want decrypted", got)
		}
		if got := lastUpstreamReq.headers.Get("X-Api-Key"); got != "sk-secret-api-key" {
			t.Errorf("upstream X-Api-Key = %q, want decrypted", got)
		}

		// Verify sealed prefixes stripped
		if lastUpstreamReq.headers.Get("X-Wardgate-Sealed-Authorization") != "" {
			t.Error("X-Wardgate-Sealed-Authorization leaked to upstream")
		}
		if lastUpstreamReq.headers.Get("X-Wardgate-Sealed-X-Api-Key") != "" {
			t.Error("X-Wardgate-Sealed-X-Api-Key leaked to upstream")
		}

		// Verify regular headers and body passed through
		if got := lastUpstreamReq.headers.Get("Content-Type"); got != "application/json" {
			t.Errorf("Content-Type not forwarded: %q", got)
		}
		if got := lastUpstreamReq.headers.Get("X-Request-Id"); got != "req-e2e-001" {
			t.Errorf("X-Request-Id not forwarded: %q", got)
		}
		if lastUpstreamReq.body != reqBody {
			t.Errorf("request body not forwarded: %q", lastUpstreamReq.body)
		}
		if lastUpstreamReq.method != "POST" {
			t.Errorf("method not forwarded: %s", lastUpstreamReq.method)
		}
		if lastUpstreamReq.path != "/resources" {
			t.Errorf("path not forwarded: %s", lastUpstreamReq.path)
		}
	})

	t.Run("invalid sealed value: valid headers still forwarded", func(t *testing.T) {
		req, _ := http.NewRequest("GET", gw.URL+"/sealed-api/data", nil)
		req.Header.Set("Authorization", "Bearer "+agentKey)
		req.Header.Set("X-Wardgate-Sealed-Authorization", sealedAuth)
		req.Header.Set("X-Wardgate-Sealed-X-Bad-Header", "invalid-not-base64!!!")

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		// Valid sealed header should still be decrypted
		if got := lastUpstreamReq.headers.Get("Authorization"); got != "Bearer ghp_realtoken123" {
			t.Errorf("valid sealed header should still be forwarded: %q", got)
		}
		// Invalid header should be skipped
		if got := lastUpstreamReq.headers.Get("X-Bad-Header"); got != "" {
			t.Errorf("invalid sealed header should be skipped, got: %q", got)
		}
	})

	t.Run("policy denied method with sealed headers returns 403", func(t *testing.T) {
		req, _ := http.NewRequest("PUT", gw.URL+"/sealed-api/resources/123", nil)
		req.Header.Set("Authorization", "Bearer "+agentKey)
		req.Header.Set("X-Wardgate-Sealed-Authorization", sealedAuth)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 for PUT (not permitted), got %d", resp.StatusCode)
		}
	})

	t.Run("sealed endpoint with custom non-auth header", func(t *testing.T) {
		sealedCustom, err := sealer.Encrypt("custom-value-123")
		if err != nil {
			t.Fatal(err)
		}

		req, _ := http.NewRequest("GET", gw.URL+"/sealed-api/data", nil)
		req.Header.Set("Authorization", "Bearer "+agentKey)
		req.Header.Set("X-Wardgate-Sealed-X-Custom-Header", sealedCustom)

		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		if got := lastUpstreamReq.headers.Get("X-Custom-Header"); got != "custom-value-123" {
			t.Errorf("custom sealed header = %q, want %q", got, "custom-value-123")
		}
		// Agent auth should have been stripped
		if got := lastUpstreamReq.headers.Get("Authorization"); strings.Contains(got, agentKey) {
			t.Error("agent auth key leaked to upstream with custom sealed header")
		}
	})
}

// mockVault implements auth.Vault for testing.
type mockVault struct {
	creds map[string]string
}

func (m *mockVault) Get(name string) (string, error) {
	if cred, ok := m.creds[name]; ok {
		return cred, nil
	}
	return "", auth.ErrCredentialNotFound
}

func randomHexKey(t *testing.T, bytes int) string {
	t.Helper()
	key := make([]byte, bytes)
	if _, err := rand.Read(key); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(key)
}
