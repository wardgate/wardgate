package smtp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wardgate/wardgate/internal/approval"
	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/policy"
)

// Test helper to create a handler with mock client
func newTestHandler() (*Handler, *mockSMTPClient) {
	client := &mockSMTPClient{}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})
	handler := NewHandler(client, engine, HandlerConfig{
		EndpointName: "smtp-test",
	})
	return handler, client
}

func TestHandler_SendEmail(t *testing.T) {
	handler, client := newTestHandler()

	body := SendRequest{
		To:      []string{"recipient@example.com"},
		Subject: "Test Subject",
		Body:    "Hello, this is a test email.",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp SendResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Status != "sent" {
		t.Errorf("expected status 'sent', got '%s'", resp.Status)
	}

	if len(client.sentEmails) != 1 {
		t.Fatalf("expected 1 sent email, got %d", len(client.sentEmails))
	}

	if client.sentEmails[0].Subject != "Test Subject" {
		t.Errorf("expected subject 'Test Subject', got '%s'", client.sentEmails[0].Subject)
	}
}

func TestHandler_SendEmailMissingRecipient(t *testing.T) {
	handler, _ := newTestHandler()

	body := SendRequest{
		Subject: "Test Subject",
		Body:    "Hello, this is a test email.",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_SendEmailMultipleRecipients(t *testing.T) {
	handler, client := newTestHandler()

	body := SendRequest{
		To:      []string{"a@example.com", "b@example.com"},
		Cc:      []string{"c@example.com"},
		Subject: "Multi-recipient test",
		Body:    "Hello all!",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if len(client.sentEmails) != 1 {
		t.Fatalf("expected 1 sent email, got %d", len(client.sentEmails))
	}

	sent := client.sentEmails[0]
	if len(sent.To) != 2 {
		t.Errorf("expected 2 To recipients, got %d", len(sent.To))
	}
	if len(sent.Cc) != 1 {
		t.Errorf("expected 1 Cc recipient, got %d", len(sent.Cc))
	}
}

func TestHandler_PolicyDeny(t *testing.T) {
	client := &mockSMTPClient{}
	engine := policy.New([]config.Rule{
		{Match: config.Match{Method: "POST"}, Action: "deny", Message: "sending not allowed"},
	})
	handler := NewHandler(client, engine, HandlerConfig{
		EndpointName: "smtp-test",
	})

	body := SendRequest{
		To:      []string{"recipient@example.com"},
		Subject: "Test Subject",
		Body:    "Hello, this is a test email.",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	if len(client.sentEmails) != 0 {
		t.Errorf("expected no sent emails, got %d", len(client.sentEmails))
	}
}

func TestHandler_RecipientAllowlist(t *testing.T) {
	client := &mockSMTPClient{}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})
	handler := NewHandler(client, engine, HandlerConfig{
		EndpointName:       "smtp-test",
		AllowedRecipients:  []string{"@allowed.com", "specific@example.com"},
	})

	tests := []struct {
		name       string
		to         []string
		expectCode int
	}{
		{
			name:       "allowed domain",
			to:         []string{"anyone@allowed.com"},
			expectCode: http.StatusOK,
		},
		{
			name:       "allowed specific",
			to:         []string{"specific@example.com"},
			expectCode: http.StatusOK,
		},
		{
			name:       "blocked recipient",
			to:         []string{"blocked@other.com"},
			expectCode: http.StatusForbidden,
		},
		{
			name:       "mixed allowed and blocked",
			to:         []string{"anyone@allowed.com", "blocked@other.com"},
			expectCode: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := SendRequest{
				To:      tt.to,
				Subject: "Test",
				Body:    "Test body",
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectCode {
				t.Errorf("expected %d, got %d: %s", tt.expectCode, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandler_AskWorkflow(t *testing.T) {
	client := &mockSMTPClient{}
	engine := policy.New([]config.Rule{
		{Match: config.Match{Method: "POST"}, Action: "ask"},
	})
	handler := NewHandler(client, engine, HandlerConfig{
		EndpointName: "smtp-test",
	})

	// Set mock approval manager that auto-approves
	handler.SetApprovalManager(&mockApprovalManager{approved: true})

	body := SendRequest{
		To:      []string{"recipient@example.com"},
		Subject: "Needs approval",
		Body:    "Please approve this email.",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "test-agent")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 after approval, got %d: %s", rec.Code, rec.Body.String())
	}

	if len(client.sentEmails) != 1 {
		t.Errorf("expected 1 sent email after approval, got %d", len(client.sentEmails))
	}
}

func TestHandler_AskWorkflowDenied(t *testing.T) {
	client := &mockSMTPClient{}
	engine := policy.New([]config.Rule{
		{Match: config.Match{Method: "POST"}, Action: "ask"},
	})
	handler := NewHandler(client, engine, HandlerConfig{
		EndpointName: "smtp-test",
	})

	// Set mock approval manager that denies
	handler.SetApprovalManager(&mockApprovalManager{approved: false})

	body := SendRequest{
		To:      []string{"recipient@example.com"},
		Subject: "Should be denied",
		Body:    "This email should not be sent.",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}

	if len(client.sentEmails) != 0 {
		t.Errorf("expected no sent emails after denial, got %d", len(client.sentEmails))
	}
}

func TestHandler_AskWorkflowNoManager(t *testing.T) {
	client := &mockSMTPClient{}
	engine := policy.New([]config.Rule{
		{Match: config.Match{Method: "POST"}, Action: "ask"},
	})
	handler := NewHandler(client, engine, HandlerConfig{
		EndpointName: "smtp-test",
	})
	// No approval manager set

	body := SendRequest{
		To:      []string{"recipient@example.com"},
		Subject: "Test",
		Body:    "Test body",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestHandler_AskNewRecipients(t *testing.T) {
	client := &mockSMTPClient{}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})
	handler := NewHandler(client, engine, HandlerConfig{
		EndpointName:      "smtp-test",
		KnownRecipients:   []string{"known@example.com", "@known-domain.com"},
		AskNewRecipients:  true,
	})
	handler.SetApprovalManager(&mockApprovalManager{approved: true})

	// Known recipient - no approval needed
	body := SendRequest{
		To:      []string{"known@example.com"},
		Subject: "To known recipient",
		Body:    "No approval needed.",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for known recipient, got %d: %s", rec.Code, rec.Body.String())
	}

	// New recipient - approval needed
	body = SendRequest{
		To:      []string{"new@unknown.com"},
		Subject: "To new recipient",
		Body:    "Approval needed.",
	}
	bodyBytes, _ = json.Marshal(body)

	req = httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()

	// Mock manager tracks if approval was requested
	mgr := &mockApprovalManager{approved: true}
	handler.SetApprovalManager(mgr)

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 after approval, got %d: %s", rec.Code, rec.Body.String())
	}

	if !mgr.approvalRequested {
		t.Error("expected approval to be requested for new recipient")
	}
}

func TestHandler_ContentFiltering(t *testing.T) {
	client := &mockSMTPClient{}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})
	handler := NewHandler(client, engine, HandlerConfig{
		EndpointName:     "smtp-test",
		BlockedKeywords:  []string{"password", "secret", "confidential"},
	})

	tests := []struct {
		name       string
		subject    string
		body       string
		expectCode int
	}{
		{
			name:       "normal email",
			subject:    "Hello",
			body:       "This is a normal email.",
			expectCode: http.StatusOK,
		},
		{
			name:       "blocked word in body",
			subject:    "Hello",
			body:       "Here is the password: 12345",
			expectCode: http.StatusForbidden,
		},
		{
			name:       "blocked word in subject",
			subject:    "Your password reset",
			body:       "Click here to reset.",
			expectCode: http.StatusForbidden,
		},
		{
			name:       "blocked word case insensitive",
			subject:    "CONFIDENTIAL info",
			body:       "Top secret stuff.",
			expectCode: http.StatusForbidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := SendRequest{
				To:      []string{"recipient@example.com"},
				Subject: tt.subject,
				Body:    tt.body,
			}
			bodyBytes, _ := json.Marshal(body)

			req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectCode {
				t.Errorf("expected %d, got %d: %s", tt.expectCode, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandler_SMTPClientError(t *testing.T) {
	client := &mockSMTPClient{failSend: true}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})
	handler := NewHandler(client, engine, HandlerConfig{
		EndpointName: "smtp-test",
	})

	body := SendRequest{
		To:      []string{"recipient@example.com"},
		Subject: "Test Subject",
		Body:    "Test body.",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

func TestHandler_InvalidJSON(t *testing.T) {
	handler, _ := newTestHandler()

	req := httptest.NewRequest("POST", "/send", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
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

func TestHandler_RateLimited(t *testing.T) {
	client := &mockSMTPClient{}
	engine := policy.New([]config.Rule{{
		Match:     config.Match{Method: "POST"},
		Action:    "allow",
		RateLimit: &config.RateLimit{Max: 1, Window: "1m"},
	}})
	handler := NewHandler(client, engine, HandlerConfig{
		EndpointName: "smtp-test",
	})

	body := SendRequest{
		To:      []string{"recipient@example.com"},
		Subject: "Test Subject",
		Body:    "Test body.",
	}
	bodyBytes, _ := json.Marshal(body)

	// First request should succeed
	req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "rate-test-agent")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("first request: expected 200, got %d", rec.Code)
	}

	// Second request should be rate limited
	req = httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "rate-test-agent")
	rec = httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d", rec.Code)
	}
}

func TestHandler_HTMLEmail(t *testing.T) {
	handler, client := newTestHandler()

	body := SendRequest{
		To:       []string{"recipient@example.com"},
		Subject:  "HTML Test",
		Body:     "Plain text version",
		HTMLBody: "<html><body><h1>HTML version</h1></body></html>",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if len(client.sentEmails) != 1 {
		t.Fatalf("expected 1 sent email, got %d", len(client.sentEmails))
	}

	if client.sentEmails[0].HTMLBody == "" {
		t.Error("expected HTML body to be set")
	}
}

func TestHandler_ReplyTo(t *testing.T) {
	handler, client := newTestHandler()

	body := SendRequest{
		To:      []string{"recipient@example.com"},
		Subject: "Reply test",
		Body:    "Please reply to the other address.",
		ReplyTo: "reply@example.com",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if client.sentEmails[0].ReplyTo != "reply@example.com" {
		t.Errorf("expected ReplyTo 'reply@example.com', got '%s'", client.sentEmails[0].ReplyTo)
	}
}

// Mock SMTP client
type mockSMTPClient struct {
	sentEmails []Email
	failSend   bool
}

func (c *mockSMTPClient) Send(ctx context.Context, email Email) error {
	if c.failSend {
		return ErrSendFailed
	}
	c.sentEmails = append(c.sentEmails, email)
	return nil
}

func (c *mockSMTPClient) Close() error {
	return nil
}

// Mock approval manager
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

// Mock approval manager for email-specific approvals
type mockEmailApprovalManager struct {
	approved          bool
	approvalRequested bool
	lastEmail         *Email
}

func (m *mockEmailApprovalManager) RequestEmailApproval(ctx context.Context, endpoint, agentID string, email *Email) (bool, error) {
	m.approvalRequested = true
	m.lastEmail = email
	return m.approved, nil
}

// Ensure handler works with known domain in recipients
func TestHandler_KnownDomainRecipients(t *testing.T) {
	client := &mockSMTPClient{}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})
	handler := NewHandler(client, engine, HandlerConfig{
		EndpointName:     "smtp-test",
		KnownRecipients:  []string{"@company.com"},
		AskNewRecipients: true,
	})
	mgr := &mockApprovalManager{approved: true}
	handler.SetApprovalManager(mgr)

	body := SendRequest{
		To:      []string{"anyone@company.com"},
		Subject: "Internal email",
		Body:    "This should not need approval.",
	}
	bodyBytes, _ := json.Marshal(body)

	req := httptest.NewRequest("POST", "/send", bytes.NewReader(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if mgr.approvalRequested {
		t.Error("should not request approval for known domain")
	}
}
