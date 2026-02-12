package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/grants"
)

func newTestExecHandler(t *testing.T) *ExecHandler {
	t.Helper()

	// Set up env for hub auth (not used in handler tests, but needed by NewHub)
	os.Setenv("TEST_CONCLAVE_KEY", "test-key")
	t.Cleanup(func() { os.Unsetenv("TEST_CONCLAVE_KEY") })

	hub := NewHub("test", map[string]ConclaveConfig{
		"obsidian": {Name: "obsidian", KeyEnv: "TEST_CONCLAVE_KEY"},
	})

	conclaves := map[string]config.ConclaveConfig{
		"obsidian": {
			Description: "Test vault",
			KeyEnv:      "TEST_CONCLAVE_KEY",
			Cwd:         "/data/vault",
			Rules: []config.Rule{
				{Match: config.Match{Command: "cat"}, Action: "allow"},
				{Match: config.Match{Command: "rg"}, Action: "allow"},
				{Match: config.Match{Command: "tee"}, Action: "ask"},
				{Match: config.Match{Command: "*"}, Action: "deny", Message: "command not allowed"},
			},
		},
	}

	return NewExecHandler(hub, conclaves)
}

func TestConclaveExecHandler_ListConclaves(t *testing.T) {
	h := newTestExecHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Conclaves []ConclaveListItem `json:"conclaves"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)

	if len(resp.Conclaves) != 1 {
		t.Fatalf("expected 1 conclave, got %d", len(resp.Conclaves))
	}
	if resp.Conclaves[0].Name != "obsidian" {
		t.Errorf("expected conclave 'obsidian', got %q", resp.Conclaves[0].Name)
	}
	if resp.Conclaves[0].Status != "disconnected" {
		t.Errorf("expected status 'disconnected', got %q", resp.Conclaves[0].Status)
	}
	if resp.Conclaves[0].Description != "Test vault" {
		t.Errorf("expected description 'Test vault', got %q", resp.Conclaves[0].Description)
	}
}

func TestConclaveExecHandler_UnknownConclave(t *testing.T) {
	h := newTestExecHandler(t)

	body := `{"raw":"cat file.txt","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/unknown/exec", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestConclaveExecHandler_PolicyDeny(t *testing.T) {
	h := newTestExecHandler(t)

	body := `{"raw":"rm -rf /","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/obsidian/exec", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	var resp ConclaveExecResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Action != "deny" {
		t.Errorf("expected action 'deny', got %q", resp.Action)
	}
}

func TestConclaveExecHandler_PolicyAllow_NotConnected(t *testing.T) {
	h := newTestExecHandler(t)

	body := `{"raw":"cat file.txt","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/obsidian/exec", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	// Allowed by policy but conclave is not connected
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}

	var resp ConclaveExecResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Action != "error" {
		t.Errorf("expected action 'error', got %q", resp.Action)
	}
	if !strings.Contains(resp.Message, "not connected") {
		t.Errorf("expected 'not connected' in message, got %q", resp.Message)
	}
}

func TestConclaveExecHandler_InvalidBody(t *testing.T) {
	h := newTestExecHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/obsidian/exec", strings.NewReader("not json"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestConclaveExecHandler_EmptyCommand(t *testing.T) {
	h := newTestExecHandler(t)

	body := `{"raw":"","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/obsidian/exec", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestConclaveExecHandler_PipelineDenyOneSegment(t *testing.T) {
	h := newTestExecHandler(t)

	// Pipeline: rg (allowed) | rm (denied) - gateway parses the raw command
	body := `{"raw":"rg TODO | rm -rf /","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/obsidian/exec", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	var resp ConclaveExecResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Action != "deny" {
		t.Errorf("expected action 'deny', got %q", resp.Action)
	}
}

func TestConclaveExecHandler_AskWithoutManager(t *testing.T) {
	h := newTestExecHandler(t)

	body := `{"raw":"tee output.txt","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/obsidian/exec", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	// Without approval manager, ask falls back to deny
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	var resp ConclaveExecResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Action != "deny" {
		t.Errorf("expected action 'deny', got %q", resp.Action)
	}
}

func TestConclaveExecHandler_AgentNotAllowed(t *testing.T) {
	// Conclave scoped to [tessa], but agent is "bob"
	os.Setenv("TEST_CONCLAVE_KEY", "test-key")
	t.Cleanup(func() { os.Unsetenv("TEST_CONCLAVE_KEY") })

	hub := NewHub("test", map[string]ConclaveConfig{
		"obsidian": {Name: "obsidian", KeyEnv: "TEST_CONCLAVE_KEY"},
	})

	conclaves := map[string]config.ConclaveConfig{
		"obsidian": {
			Description: "Test vault",
			KeyEnv:      "TEST_CONCLAVE_KEY",
			Cwd:         "/data/vault",
			Agents:      []string{"tessa"},
			Rules: []config.Rule{
				{Match: config.Match{Command: "cat"}, Action: "allow"},
				{Match: config.Match{Command: "*"}, Action: "deny"},
			},
		},
	}

	h := NewExecHandler(hub, conclaves)

	body := `{"raw":"cat file.txt","agent_id":"bob"}`
	req := httptest.NewRequest(http.MethodPost, "/obsidian/exec", strings.NewReader(body))
	req.Header.Set("X-Agent-ID", "bob")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	var resp ConclaveExecResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Action != "deny" {
		t.Errorf("expected action 'deny', got %q", resp.Action)
	}
	if !strings.Contains(resp.Message, "not allowed") {
		t.Errorf("expected 'not allowed' in message, got %q", resp.Message)
	}
}

func TestConclaveExecHandler_AgentAllowed(t *testing.T) {
	// Conclave scoped to [tessa], agent is "tessa" -- should proceed to policy evaluation
	os.Setenv("TEST_CONCLAVE_KEY", "test-key")
	t.Cleanup(func() { os.Unsetenv("TEST_CONCLAVE_KEY") })

	hub := NewHub("test", map[string]ConclaveConfig{
		"obsidian": {Name: "obsidian", KeyEnv: "TEST_CONCLAVE_KEY"},
	})

	conclaves := map[string]config.ConclaveConfig{
		"obsidian": {
			Description: "Test vault",
			KeyEnv:      "TEST_CONCLAVE_KEY",
			Cwd:         "/data/vault",
			Agents:      []string{"tessa"},
			Rules: []config.Rule{
				{Match: config.Match{Command: "cat"}, Action: "allow"},
				{Match: config.Match{Command: "*"}, Action: "deny"},
			},
		},
	}

	h := NewExecHandler(hub, conclaves)

	body := `{"raw":"cat file.txt","agent_id":"tessa"}`
	req := httptest.NewRequest(http.MethodPost, "/obsidian/exec", strings.NewReader(body))
	req.Header.Set("X-Agent-ID", "tessa")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	// Should NOT get 403 for agent scoping -- should proceed to policy (allow cat) then fail on not-connected
	if rec.Code == http.StatusForbidden {
		t.Errorf("agent 'tessa' should be allowed, got 403")
	}
	// Expect 503 (not connected) since policy allows cat but conclave is offline
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 (not connected), got %d", rec.Code)
	}
}

func TestConclaveExecHandler_AgentScopeListFiltered(t *testing.T) {
	// List conclaves should only show conclaves the agent is allowed to access
	os.Setenv("TEST_CONCLAVE_KEY", "test-key")
	os.Setenv("TEST_CONCLAVE_KEY2", "test-key2")
	t.Cleanup(func() {
		os.Unsetenv("TEST_CONCLAVE_KEY")
		os.Unsetenv("TEST_CONCLAVE_KEY2")
	})

	hub := NewHub("test", map[string]ConclaveConfig{
		"obsidian": {Name: "obsidian", KeyEnv: "TEST_CONCLAVE_KEY"},
		"code":     {Name: "code", KeyEnv: "TEST_CONCLAVE_KEY2"},
	})

	conclaves := map[string]config.ConclaveConfig{
		"obsidian": {
			Description: "Test vault",
			KeyEnv:      "TEST_CONCLAVE_KEY",
			Agents:      []string{"tessa"},
			Rules:       []config.Rule{{Match: config.Match{Command: "*"}, Action: "deny"}},
		},
		"code": {
			Description: "Code env",
			KeyEnv:      "TEST_CONCLAVE_KEY2",
			Agents:      []string{"bob"},
			Rules:       []config.Rule{{Match: config.Match{Command: "*"}, Action: "deny"}},
		},
	}

	h := NewExecHandler(hub, conclaves)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("X-Agent-ID", "tessa")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp struct {
		Conclaves []ConclaveListItem `json:"conclaves"`
	}
	json.NewDecoder(rec.Body).Decode(&resp)

	if len(resp.Conclaves) != 1 {
		t.Fatalf("expected 1 conclave for tessa, got %d", len(resp.Conclaves))
	}
	if resp.Conclaves[0].Name != "obsidian" {
		t.Errorf("expected conclave 'obsidian', got %q", resp.Conclaves[0].Name)
	}
}

func TestConclaveExecHandler_GrantOverridesPolicy(t *testing.T) {
	// Static policy denies "rm", but an active grant for "rm" on conclave:obsidian should allow it
	os.Setenv("TEST_CONCLAVE_KEY", "test-key")
	t.Cleanup(func() { os.Unsetenv("TEST_CONCLAVE_KEY") })

	h := newTestExecHandler(t)

	// Add a grant that allows "rm" on conclave:obsidian
	grantStore := grants.NewStore("")
	grantStore.Add(grants.Grant{
		AgentID: "agent-1",
		Scope:   "conclave:obsidian",
		Match:   grants.GrantMatch{Command: "rm"},
		Action:  "allow",
		Reason:  "test grant",
	})
	h.SetGrantStore(grantStore)

	body := `{"raw":"rm -rf /tmp/test","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/obsidian/exec", strings.NewReader(body))
	req.Header.Set("X-Agent-ID", "agent-1")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	// Should NOT be 403 (denied by policy) -- grant should override
	// Expect 503 (not connected) since the conclave is offline
	if rec.Code == http.StatusForbidden {
		t.Error("grant should override static policy deny")
	}
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 (not connected), got %d", rec.Code)
	}
}

func TestConclaveExecHandler_ExpiredGrantFallsThrough(t *testing.T) {
	os.Setenv("TEST_CONCLAVE_KEY", "test-key")
	t.Cleanup(func() { os.Unsetenv("TEST_CONCLAVE_KEY") })

	h := newTestExecHandler(t)

	// Add an expired grant
	expired := time.Now().Add(-1 * time.Hour)
	grantStore := grants.NewStore("")
	grantStore.Add(grants.Grant{
		AgentID:   "agent-1",
		Scope:     "conclave:obsidian",
		Match:     grants.GrantMatch{Command: "rm"},
		Action:    "allow",
		ExpiresAt: &expired,
	})
	h.SetGrantStore(grantStore)

	body := `{"raw":"rm -rf /","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/obsidian/exec", strings.NewReader(body))
	req.Header.Set("X-Agent-ID", "agent-1")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	// Expired grant should not override -- static policy should deny
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 (expired grant falls through to deny), got %d", rec.Code)
	}
}

func TestConclaveExecHandler_MethodNotAllowed(t *testing.T) {
	h := newTestExecHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/obsidian/exec", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}
