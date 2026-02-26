package e2e

import (
	"bufio"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wardgate/wardgate/internal/auth"
	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/filter"
	"github.com/wardgate/wardgate/internal/policy"
	"github.com/wardgate/wardgate/internal/proxy"
)

// TestSSE_E2E exercises the full SSE streaming path through the gateway stack:
//
//	agent (bearer auth) → agent auth middleware → policy engine → proxy (filter) → upstream SSE
//
// This covers all SSE filter modes: redact, block, passthrough, and verifies
// that sensitive data never leaks to the agent.
func TestSSE_E2E(t *testing.T) {
	agentKey := "test-sse-agent-key-" + randomHexKey(t, 8)
	agentKeyEnv := "TEST_SSE_AGENT_KEY"
	credEnv := "TEST_SSE_CRED"

	t.Setenv(agentKeyEnv, agentKey)
	t.Setenv(credEnv, "upstream-secret")

	agents := []config.AgentConfig{
		{ID: "sse-agent", KeyEnv: agentKeyEnv},
	}

	allowAll := []config.Rule{
		{Match: config.Match{Method: "*"}, Action: "allow"},
	}

	t.Run("redact: sensitive data replaced in SSE stream", func(t *testing.T) {
		// Upstream sends SSE with an API key in one of the messages
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.Header().Set("Cache-Control", "no-cache")
			w.WriteHeader(http.StatusOK)
			// Message 1: clean
			w.Write([]byte("data: {\"text\": \"hello\"}\n\n"))
			// Message 2: contains API key
			w.Write([]byte("data: {\"key\": \"sk-1234567890abcdef1234567890abcdef\"}\n\n"))
			// Message 3: clean sentinel
			w.Write([]byte("data: [DONE]\n\n"))
		}))
		defer upstream.Close()

		f := newSSEFilter(t, filter.ActionRedact, "filter")
		gw := buildSSEGateway(t, "sse-redact", upstream.URL, agents, allowAll, credEnv, f)
		defer gw.Close()

		resp := doSSERequest(t, gw, agentKey, "/sse-redact/stream")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		events := readSSEEvents(t, resp.Body)

		// Should have 3 events
		if len(events) < 3 {
			t.Fatalf("expected at least 3 SSE events, got %d: %v", len(events), events)
		}

		// First event: clean, passes through
		if !strings.Contains(events[0], "hello") {
			t.Errorf("first event should contain 'hello', got: %s", events[0])
		}

		// Second event: API key should be redacted
		if strings.Contains(events[1], "sk-1234567890abcdef1234567890abcdef") {
			t.Error("API key leaked through redaction filter to agent")
		}
		if !strings.Contains(events[1], "[REDACTED]") {
			t.Error("expected redaction marker in second event")
		}

		// Third event: [DONE] sentinel passes through
		if !strings.Contains(events[2], "[DONE]") {
			t.Errorf("expected [DONE] sentinel, got: %s", events[2])
		}
	})

	t.Run("block: stream terminated on sensitive data", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			// Message 1: clean
			w.Write([]byte("data: {\"text\": \"hello\"}\n\n"))
			// Message 2: contains API key → should trigger block
			w.Write([]byte("data: {\"key\": \"sk-1234567890abcdef1234567890abcdef\"}\n\n"))
			// Message 3: should never reach agent
			w.Write([]byte("data: {\"text\": \"after block\"}\n\n"))
		}))
		defer upstream.Close()

		f := newSSEFilter(t, filter.ActionBlock, "filter")
		gw := buildSSEGateway(t, "sse-block", upstream.URL, agents, allowAll, credEnv, f)
		defer gw.Close()

		resp := doSSERequest(t, gw, agentKey, "/sse-block/stream")
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// First clean event should pass through
		if !strings.Contains(bodyStr, "hello") {
			t.Error("expected first clean event to pass through")
		}

		// Should contain error event
		if !strings.Contains(bodyStr, "event: error") {
			t.Error("expected error event when stream is blocked")
		}
		if !strings.Contains(bodyStr, "response blocked") {
			t.Error("expected 'response blocked' in error event")
		}

		// Error event must NOT leak filter pattern names
		if strings.Contains(bodyStr, "api_keys") {
			t.Error("error event leaks filter pattern name 'api_keys' to agent")
		}

		// Content after block should not reach agent
		if strings.Contains(bodyStr, "after block") {
			t.Error("content after block event should not reach agent")
		}
	})

	t.Run("block error does not leak pattern names", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("data: {\"key\": \"sk-1234567890abcdef1234567890abcdef\"}\n\n"))
		}))
		defer upstream.Close()

		f := newSSEFilter(t, filter.ActionBlock, "filter")
		gw := buildSSEGateway(t, "sse-noleak", upstream.URL, agents, allowAll, credEnv, f)
		defer gw.Close()

		resp := doSSERequest(t, gw, agentKey, "/sse-noleak/stream")
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// Must not contain any filter pattern name hints
		for _, forbidden := range []string{"api_keys", "otp_codes", "verification_links", "sensitive data detected"} {
			if strings.Contains(bodyStr, forbidden) {
				t.Errorf("error event leaks filter metadata %q to agent", forbidden)
			}
		}

		// Must contain the generic error
		if !strings.Contains(bodyStr, `"error":"response blocked"`) {
			t.Errorf("expected generic error message, got: %s", bodyStr)
		}
	})

	t.Run("passthrough mode: no filtering on SSE", func(t *testing.T) {
		sseData := "data: {\"key\": \"sk-1234567890abcdef1234567890abcdef\"}\n\ndata: [DONE]\n\n"
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(sseData))
		}))
		defer upstream.Close()

		f := newSSEFilter(t, filter.ActionBlock, "passthrough")
		gw := buildSSEGateway(t, "sse-pass", upstream.URL, agents, allowAll, credEnv, f)
		defer gw.Close()

		resp := doSSERequest(t, gw, agentKey, "/sse-pass/stream")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if string(body) != sseData {
			t.Errorf("passthrough mode should not alter SSE data\ngot:  %q\nwant: %q", string(body), sseData)
		}
	})

	t.Run("non-SSE response still blocked by filter", func(t *testing.T) {
		// Verify that enabling SSE filtering doesn't break non-SSE filter behavior
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"key": "sk-1234567890abcdef1234567890abcdef"}`))
		}))
		defer upstream.Close()

		f := newSSEFilter(t, filter.ActionBlock, "filter")
		gw := buildSSEGateway(t, "sse-nonjson", upstream.URL, agents, allowAll, credEnv, f)
		defer gw.Close()

		resp := doSSERequest(t, gw, agentKey, "/sse-nonjson/data")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 for blocked non-SSE response, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		// Non-SSE block should also use generic message (no pattern leak)
		if strings.Contains(bodyStr, "api_keys") {
			t.Error("non-SSE block error leaks filter pattern name to agent")
		}
	})

	t.Run("policy deny still works for SSE endpoints", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("upstream should not be reached when policy denies")
		}))
		defer upstream.Close()

		denyDelete := []config.Rule{
			{Match: config.Match{Method: "GET"}, Action: "allow"},
			{Match: config.Match{Method: "*"}, Action: "deny", Message: "read only"},
		}

		f := newSSEFilter(t, filter.ActionRedact, "filter")
		gw := buildSSEGateway(t, "sse-deny", upstream.URL, agents, denyDelete, credEnv, f)
		defer gw.Close()

		req, _ := http.NewRequest("POST", gw.URL+"/sse-deny/stream", nil)
		req.Header.Set("Authorization", "Bearer "+agentKey)
		resp, err := gw.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("expected 403 for policy deny, got %d", resp.StatusCode)
		}
	})

	t.Run("unauthenticated request to SSE endpoint returns 401", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("upstream should not be reached without auth")
		}))
		defer upstream.Close()

		f := newSSEFilter(t, filter.ActionRedact, "filter")
		gw := buildSSEGateway(t, "sse-noauth", upstream.URL, agents, allowAll, credEnv, f)
		defer gw.Close()

		req, _ := http.NewRequest("GET", gw.URL+"/sse-noauth/stream", nil)
		// No Authorization header
		resp, err := gw.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("SSE metadata lines preserved through filter", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("id: msg-001\nevent: delta\nretry: 3000\ndata: {\"text\": \"hello\"}\n\n"))
		}))
		defer upstream.Close()

		f := newSSEFilter(t, filter.ActionRedact, "filter")
		gw := buildSSEGateway(t, "sse-meta", upstream.URL, agents, allowAll, credEnv, f)
		defer gw.Close()

		resp := doSSERequest(t, gw, agentKey, "/sse-meta/stream")
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		for _, expected := range []string{"id: msg-001", "event: delta", "retry: 3000"} {
			if !strings.Contains(bodyStr, expected) {
				t.Errorf("expected SSE metadata %q preserved, got: %s", expected, bodyStr)
			}
		}
	})

	t.Run("multi-message stream: sensitive data mid-stream", func(t *testing.T) {
		// Simulates a realistic scenario: LLM streaming where an API key
		// appears partway through the response chunks.
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			// 5 clean messages, then one with sensitive data, then sentinel
			for i := 0; i < 5; i++ {
				w.Write([]byte(`data: {"choices":[{"delta":{"content":"word "}}]}` + "\n\n"))
			}
			w.Write([]byte(`data: {"choices":[{"delta":{"content":"key: sk-1234567890abcdef1234567890abcdef"}}]}` + "\n\n"))
			w.Write([]byte("data: [DONE]\n\n"))
		}))
		defer upstream.Close()

		f := newSSEFilter(t, filter.ActionRedact, "filter")
		gw := buildSSEGateway(t, "sse-multi", upstream.URL, agents, allowAll, credEnv, f)
		defer gw.Close()

		resp := doSSERequest(t, gw, agentKey, "/sse-multi/stream")
		defer resp.Body.Close()

		events := readSSEEvents(t, resp.Body)

		// Should have 7 events (5 clean + 1 redacted + 1 DONE)
		if len(events) != 7 {
			t.Fatalf("expected 7 SSE events, got %d", len(events))
		}

		// First 5 should pass through with "word "
		for i := 0; i < 5; i++ {
			if !strings.Contains(events[i], "word ") {
				t.Errorf("event %d should contain 'word ', got: %s", i, events[i])
			}
		}

		// 6th event should have API key redacted
		if strings.Contains(events[5], "sk-1234567890abcdef1234567890abcdef") {
			t.Error("API key leaked in mid-stream event")
		}
		if !strings.Contains(events[5], "[REDACTED]") {
			t.Error("expected redaction marker in mid-stream event")
		}

		// 7th event: DONE
		if !strings.Contains(events[6], "[DONE]") {
			t.Errorf("expected [DONE] sentinel, got: %s", events[6])
		}
	})
}

// TestSSE_DynamicUpstream_E2E tests SSE streaming through dynamic upstreams.
func TestSSE_DynamicUpstream_E2E(t *testing.T) {
	agentKey := "test-dyn-sse-key-" + randomHexKey(t, 8)
	agentKeyEnv := "TEST_DYN_SSE_AGENT_KEY"
	credEnv := "TEST_DYN_SSE_CRED"

	t.Setenv(agentKeyEnv, agentKey)
	t.Setenv(credEnv, "upstream-token")

	agents := []config.AgentConfig{
		{ID: "dyn-agent", KeyEnv: agentKeyEnv},
	}

	t.Run("SSE stream via dynamic upstream with filtering", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("data: {\"key\": \"sk-1234567890abcdef1234567890abcdef\"}\n\n"))
			w.Write([]byte("data: [DONE]\n\n"))
		}))
		defer upstream.Close()

		f := newSSEFilter(t, filter.ActionRedact, "filter")

		engine := policy.New([]config.Rule{
			{Match: config.Match{Method: "*"}, Action: "allow"},
		})

		vault := &mockVault{creds: map[string]string{credEnv: "upstream-token"}}
		endpoint := config.Endpoint{
			// No static upstream — must use dynamic
			AllowedUpstreams: []string{upstream.URL},
			Auth:             config.AuthConfig{Type: "bearer", CredentialEnv: credEnv},
		}

		p := proxy.NewWithName("dyn-sse", endpoint, vault, engine)
		p.SetFilter(f)

		apiMux := http.NewServeMux()
		apiMux.Handle("/dyn-sse/", http.StripPrefix("/dyn-sse", p))
		gw := httptest.NewServer(auth.NewAgentAuthMiddleware(agents, nil, apiMux))
		defer gw.Close()

		req, _ := http.NewRequest("GET", gw.URL+"/dyn-sse/stream", nil)
		req.Header.Set("Authorization", "Bearer "+agentKey)
		req.Header.Set("X-Wardgate-Upstream", upstream.URL)

		resp, err := gw.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}

		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)

		if strings.Contains(bodyStr, "sk-1234567890abcdef1234567890abcdef") {
			t.Error("API key leaked via dynamic upstream SSE stream")
		}
		if !strings.Contains(bodyStr, "[REDACTED]") {
			t.Error("expected redaction in dynamic upstream SSE stream")
		}
		if !strings.Contains(bodyStr, "[DONE]") {
			t.Error("expected [DONE] sentinel to pass through")
		}
	})

	t.Run("dynamic upstream denied rejects before streaming", func(t *testing.T) {
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Error("upstream should not be reached for denied dynamic upstream")
		}))
		defer upstream.Close()

		engine := policy.New([]config.Rule{
			{Match: config.Match{Method: "*"}, Action: "allow"},
		})

		vault := &mockVault{creds: map[string]string{credEnv: "upstream-token"}}
		endpoint := config.Endpoint{
			AllowedUpstreams: []string{"https://api.allowed.com"},
			Auth:             config.AuthConfig{Type: "bearer", CredentialEnv: credEnv},
		}

		p := proxy.NewWithName("dyn-deny", endpoint, vault, engine)

		apiMux := http.NewServeMux()
		apiMux.Handle("/dyn-deny/", http.StripPrefix("/dyn-deny", p))
		gw := httptest.NewServer(auth.NewAgentAuthMiddleware(agents, nil, apiMux))
		defer gw.Close()

		req, _ := http.NewRequest("GET", gw.URL+"/dyn-deny/stream", nil)
		req.Header.Set("Authorization", "Bearer "+agentKey)
		req.Header.Set("X-Wardgate-Upstream", upstream.URL) // not in allowed list

		resp, err := gw.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("expected 400 for denied dynamic upstream, got %d", resp.StatusCode)
		}
	})
}

// TestSSE_ScannerBufferLimit tests that oversized SSE lines are handled gracefully.
func TestSSE_ScannerBufferLimit(t *testing.T) {
	agentKey := "test-buf-key-" + randomHexKey(t, 8)
	agentKeyEnv := "TEST_BUF_AGENT_KEY"
	credEnv := "TEST_BUF_CRED"

	t.Setenv(agentKeyEnv, agentKey)
	t.Setenv(credEnv, "token")

	agents := []config.AgentConfig{
		{ID: "buf-agent", KeyEnv: agentKeyEnv},
	}
	allowAll := []config.Rule{
		{Match: config.Match{Method: "*"}, Action: "allow"},
	}

	t.Run("normal large SSE message within buffer limit", func(t *testing.T) {
		// 100KB data line — well within 1MB limit
		largePayload := strings.Repeat("x", 100*1024)
		sseData := "data: " + largePayload + "\n\n"

		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(sseData))
		}))
		defer upstream.Close()

		f := newSSEFilter(t, filter.ActionRedact, "filter")
		gw := buildSSEGateway(t, "sse-bigok", upstream.URL, agents, allowAll, credEnv, f)
		defer gw.Close()

		resp := doSSERequest(t, gw, agentKey, "/sse-bigok/stream")
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for large-but-within-limit SSE, got %d", resp.StatusCode)
		}

		body, _ := io.ReadAll(resp.Body)
		if len(body) < 100*1024 {
			t.Errorf("expected large payload to pass through, got %d bytes", len(body))
		}
	})

	t.Run("oversized SSE line exceeding buffer limit", func(t *testing.T) {
		// 2MB data line — exceeds 1MB scanner limit
		hugePayload := strings.Repeat("x", 2*1024*1024)
		sseData := "data: " + hugePayload + "\n\n"

		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(sseData))
		}))
		defer upstream.Close()

		f := newSSEFilter(t, filter.ActionRedact, "filter")
		gw := buildSSEGateway(t, "sse-huge", upstream.URL, agents, allowAll, credEnv, f)
		defer gw.Close()

		resp := doSSERequest(t, gw, agentKey, "/sse-huge/stream")
		defer resp.Body.Close()

		// The response should complete without hanging. The scanner will hit
		// its buffer limit and return an error, which surfaces as a truncated
		// or empty body (the SSE reader sets done=true on scanner error).
		body, _ := io.ReadAll(resp.Body)

		// The key assertion: the oversized line should NOT pass through intact.
		// Either the body is truncated or empty — the gateway handled it gracefully.
		if len(body) >= 2*1024*1024 {
			t.Error("oversized SSE line should not pass through the filter intact")
		}
	})
}

// --- helpers ---

func newSSEFilter(t *testing.T, action filter.Action, sseMode string) *filter.Filter {
	t.Helper()
	f, err := filter.New(filter.Config{
		Enabled:     true,
		Patterns:    []string{"api_keys"},
		Action:      action,
		Replacement: "[REDACTED]",
		SSEMode:     sseMode,
	})
	if err != nil {
		t.Fatalf("failed to create filter: %v", err)
	}
	return f
}

// buildSSEGateway wires up a full gateway stack for SSE testing, mirroring main.go.
func buildSSEGateway(
	t *testing.T,
	name string,
	upstreamURL string,
	agents []config.AgentConfig,
	rules []config.Rule,
	credEnv string,
	f *filter.Filter,
) *httptest.Server {
	t.Helper()

	engine := policy.New(rules)
	vault := &mockVault{creds: map[string]string{credEnv: "upstream-token"}}

	endpoint := config.Endpoint{
		Upstream: upstreamURL,
		Auth:     config.AuthConfig{Type: "bearer", CredentialEnv: credEnv},
	}

	p := proxy.NewWithName(name, endpoint, vault, engine)
	if f != nil {
		p.SetFilter(f)
	}

	apiMux := http.NewServeMux()
	apiMux.Handle("/"+name+"/", http.StripPrefix("/"+name, p))

	gw := httptest.NewServer(auth.NewAgentAuthMiddleware(agents, nil, apiMux))
	t.Cleanup(gw.Close)
	return gw
}

func doSSERequest(t *testing.T, gw *httptest.Server, agentKey, path string) *http.Response {
	t.Helper()
	req, _ := http.NewRequest("GET", gw.URL+path, nil)
	req.Header.Set("Authorization", "Bearer "+agentKey)
	resp, err := gw.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// readSSEEvents parses SSE events from the response body.
// Each event is the raw text between blank-line delimiters.
func readSSEEvents(t *testing.T, body io.Reader) []string {
	t.Helper()
	var events []string
	var current strings.Builder

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			// Blank line = end of event
			if current.Len() > 0 {
				events = append(events, current.String())
				current.Reset()
			}
			continue
		}
		if current.Len() > 0 {
			current.WriteString("\n")
		}
		current.WriteString(line)
	}
	// Capture any trailing event without final blank line
	if current.Len() > 0 {
		events = append(events, current.String())
	}
	return events
}
