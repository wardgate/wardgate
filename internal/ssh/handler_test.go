package ssh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wardgate/wardgate/internal/approval"
	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/policy"
)

func newTestHandler() (*Handler, *mockPool) {
	pool := &mockPool{client: &mockClient{}}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})
	handler := NewHandler(pool, engine, HandlerConfig{
		EndpointName: "ssh-test",
		ConnectionConfig: ConnectionConfig{
			Host:     "prod.example.com",
			Port:     22,
			Username: "deploy",
		},
	})
	return handler, pool
}

func TestHandler_ExecCommand(t *testing.T) {
	handler, pool := newTestHandler()
	pool.client.stdout = "hello world\n"

	body := ExecRequest{Command: "echo hello world"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ExecResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Stdout != "hello world\n" {
		t.Errorf("expected stdout 'hello world\\n', got %q", resp.Stdout)
	}
	if resp.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", resp.ExitCode)
	}
}

func TestHandler_ExecCommandWithCwd(t *testing.T) {
	handler, pool := newTestHandler()
	pool.client.stdout = "/opt/app\n"

	body := ExecRequest{Command: "pwd", Cwd: "/opt/app"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if pool.client.lastCwd != "/opt/app" {
		t.Errorf("expected cwd '/opt/app', got %q", pool.client.lastCwd)
	}
}

func TestHandler_ExecCommandWithStderr(t *testing.T) {
	handler, pool := newTestHandler()
	pool.client.stderr = "warning: something\n"
	pool.client.exitCode = 1

	body := ExecRequest{Command: "failing-command"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp ExecResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)

	if resp.Stderr != "warning: something\n" {
		t.Errorf("expected stderr, got %q", resp.Stderr)
	}
	if resp.ExitCode != 1 {
		t.Errorf("expected exit code 1, got %d", resp.ExitCode)
	}
}

func TestHandler_ExecMissingCommand(t *testing.T) {
	handler, _ := newTestHandler()

	body := ExecRequest{}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_ExecInvalidJSON(t *testing.T) {
	handler, _ := newTestHandler()

	req := httptest.NewRequest("POST", "/exec", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_PolicyDeny(t *testing.T) {
	pool := &mockPool{client: &mockClient{}}
	engine := policy.New([]config.Rule{
		{Match: config.Match{Method: "POST"}, Action: "deny", Message: "exec not allowed"},
	})
	handler := NewHandler(pool, engine, HandlerConfig{EndpointName: "ssh-test"})

	body := ExecRequest{Command: "ls"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	if pool.client.execCount > 0 {
		t.Error("command should not have been executed")
	}
}

func TestHandler_PolicyRateLimited(t *testing.T) {
	pool := &mockPool{client: &mockClient{}}
	engine := policy.New([]config.Rule{{
		Match:     config.Match{Method: "POST"},
		Action:    "allow",
		RateLimit: &config.RateLimit{Max: 1, Window: "1m"},
	}})
	handler := NewHandler(pool, engine, HandlerConfig{
		EndpointName:     "ssh-test",
		ConnectionConfig: ConnectionConfig{Host: "prod.example.com", Port: 22, Username: "deploy"},
	})

	body := ExecRequest{Command: "ls"}
	bodyBytes, _ := json.Marshal(body)

	// First request succeeds
	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "rate-test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("first request: expected 200, got %d", rec.Code)
	}

	// Second request is rate limited
	req = httptest.NewRequest("POST", "/exec", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "rate-test")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d", rec.Code)
	}
}

func TestHandler_AskWorkflowApproved(t *testing.T) {
	pool := &mockPool{client: &mockClient{stdout: "ok\n"}}
	engine := policy.New([]config.Rule{
		{Match: config.Match{Method: "POST"}, Action: "ask"},
	})
	handler := NewHandler(pool, engine, HandlerConfig{
		EndpointName:     "ssh-test",
		ConnectionConfig: ConnectionConfig{Host: "prod.example.com", Port: 22, Username: "deploy"},
	})
	handler.SetApprovalManager(&mockApprovalManager{approved: true})

	body := ExecRequest{Command: "systemctl restart nginx"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "test-agent")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 after approval, got %d: %s", rec.Code, rec.Body.String())
	}

	if pool.client.execCount != 1 {
		t.Errorf("expected 1 exec after approval, got %d", pool.client.execCount)
	}
}

func TestHandler_AskWorkflowDenied(t *testing.T) {
	pool := &mockPool{client: &mockClient{}}
	engine := policy.New([]config.Rule{
		{Match: config.Match{Method: "POST"}, Action: "ask"},
	})
	handler := NewHandler(pool, engine, HandlerConfig{EndpointName: "ssh-test"})
	handler.SetApprovalManager(&mockApprovalManager{approved: false})

	body := ExecRequest{Command: "rm -rf /"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	if pool.client.execCount > 0 {
		t.Error("command should not have been executed after denial")
	}
}

func TestHandler_AskWorkflowNoManager(t *testing.T) {
	pool := &mockPool{client: &mockClient{}}
	engine := policy.New([]config.Rule{
		{Match: config.Match{Method: "POST"}, Action: "ask"},
	})
	handler := NewHandler(pool, engine, HandlerConfig{EndpointName: "ssh-test"})

	body := ExecRequest{Command: "ls"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestHandler_ConnectionError(t *testing.T) {
	pool := &mockPool{getErr: ErrConnectionFailed}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})
	handler := NewHandler(pool, engine, HandlerConfig{
		EndpointName:     "ssh-test",
		ConnectionConfig: ConnectionConfig{Host: "prod.example.com", Port: 22, Username: "deploy"},
	})

	body := ExecRequest{Command: "ls"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

func TestHandler_ExecError(t *testing.T) {
	pool := &mockPool{client: &mockClient{execErr: fmt.Errorf("connection reset")}}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})
	handler := NewHandler(pool, engine, HandlerConfig{
		EndpointName:     "ssh-test",
		ConnectionConfig: ConnectionConfig{Host: "prod.example.com", Port: 22, Username: "deploy"},
	})

	body := ExecRequest{Command: "ls"}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/exec", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

func TestHandler_NotFound(t *testing.T) {
	handler, _ := newTestHandler()

	req := httptest.NewRequest("GET", "/unknown", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestHandler_WrongMethod(t *testing.T) {
	handler, _ := newTestHandler()

	req := httptest.NewRequest("GET", "/exec", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// Mock implementations

type mockPool struct {
	client *mockClient
	getErr error
}

func (p *mockPool) Get(endpoint string, cfg ConnectionConfig) (Client, error) {
	if p.getErr != nil {
		return nil, p.getErr
	}
	return p.client, nil
}

func (p *mockPool) Put(endpoint string, client Client) {}

type mockClient struct {
	stdout    string
	stderr    string
	exitCode  int
	execErr   error
	execCount int
	lastCmd   string
	lastCwd   string
	alive     bool
}

func (c *mockClient) Exec(ctx context.Context, command string, cwd string) (string, string, int, error) {
	c.execCount++
	c.lastCmd = command
	c.lastCwd = cwd
	if c.execErr != nil {
		return "", "", -1, c.execErr
	}
	return c.stdout, c.stderr, c.exitCode, nil
}

func (c *mockClient) Close() error  { return nil }
func (c *mockClient) IsAlive() bool { return !c.alive }

type mockApprovalManager struct {
	approved          bool
	approvalRequested bool
}

func (m *mockApprovalManager) RequestApproval(ctx context.Context, endpoint, method, path, agentID string) (bool, error) {
	m.approvalRequested = true
	return m.approved, nil
}

func (m *mockApprovalManager) RequestApprovalWithContent(ctx context.Context, req approval.ApprovalRequest) (bool, error) {
	m.approvalRequested = true
	return m.approved, nil
}
