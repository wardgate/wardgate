package exec

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/policy"
)

func newTestHandler() *Handler {
	rules := []config.Rule{
		{Match: config.Match{Command: "cat"}, Action: "allow"},
		{Match: config.Match{Command: "rg"}, Action: "allow"},
		{Match: config.Match{Command: "tee"}, Action: "ask"},
		{Match: config.Match{Command: "*"}, Action: "deny", Message: "command not allowed"},
	}
	engine := policy.New(rules)
	return NewHandler(engine, "test-conclave")
}

func TestExecHandler_Evaluate_Allow(t *testing.T) {
	h := newTestHandler()
	body := `{"command":"cat","args":"file.txt","cwd":"/data","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/evaluate", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var resp EvaluateResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Action != "allow" {
		t.Errorf("expected action 'allow', got %q", resp.Action)
	}
}

func TestExecHandler_Evaluate_Deny(t *testing.T) {
	h := newTestHandler()
	body := `{"command":"rm","args":"-rf /","cwd":"/","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/evaluate", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	var resp EvaluateResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Action != "deny" {
		t.Errorf("expected action 'deny', got %q", resp.Action)
	}
}

func TestExecHandler_Evaluate_AskWithoutManager(t *testing.T) {
	h := newTestHandler()
	body := `{"command":"tee","args":"output.txt","cwd":"/data","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/evaluate", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	// Without approval manager, ask should fall back to deny
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	var resp EvaluateResponse
	json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Action != "deny" {
		t.Errorf("expected action 'deny', got %q", resp.Action)
	}
}

func TestExecHandler_Evaluate_EmptyCommand(t *testing.T) {
	h := newTestHandler()
	body := `{"command":"","agent_id":"agent-1"}`
	req := httptest.NewRequest(http.MethodPost, "/evaluate", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestExecHandler_Evaluate_InvalidBody(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodPost, "/evaluate", strings.NewReader("not json"))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestExecHandler_Report_OK(t *testing.T) {
	h := newTestHandler()
	body := `{"command":"cat","args":"file.txt","cwd":"/data","agent_id":"agent-1","exit_code":0,"duration_ms":50}`
	req := httptest.NewRequest(http.MethodPost, "/report", strings.NewReader(body))
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestExecHandler_NotFound(t *testing.T) {
	h := newTestHandler()
	req := httptest.NewRequest(http.MethodGet, "/unknown", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestExecHandler_Evaluate_AgentIDFromHeader(t *testing.T) {
	h := newTestHandler()
	body := `{"command":"cat","args":"file.txt","cwd":"/data"}`
	req := httptest.NewRequest(http.MethodPost, "/evaluate", strings.NewReader(body))
	req.Header.Set("X-Agent-ID", "header-agent")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}
