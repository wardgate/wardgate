package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/wardgate/wardgate/internal/config"
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

	body := `{"command":"cat","args":"file.txt","raw":"cat file.txt","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/unknown/exec", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestConclaveExecHandler_PolicyDeny(t *testing.T) {
	h := newTestExecHandler(t)

	body := `{"command":"rm","args":"-rf /","raw":"rm -rf /","agent_id":"agent-1"}`
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

	body := `{"command":"cat","args":"file.txt","raw":"cat file.txt","agent_id":"agent-1"}`
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

func TestConclaveExecHandler_EmptySegments(t *testing.T) {
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

	// Pipeline: rg (allowed) | rm (denied)
	body := `{"segments":[{"command":"rg","args":"TODO"},{"command":"rm","args":"-rf /"}],"raw":"rg TODO | rm -rf /","agent_id":"agent-1"}`
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

	body := `{"command":"tee","args":"output.txt","raw":"tee output.txt","agent_id":"agent-1"}`
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

func TestConclaveExecHandler_MethodNotAllowed(t *testing.T) {
	h := newTestExecHandler(t)

	req := httptest.NewRequest(http.MethodPut, "/obsidian/exec", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405, got %d", rec.Code)
	}
}
