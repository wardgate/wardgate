package imap

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/policy"
)

// Test helper to create a handler with mock pool
func newTestHandler() (*Handler, *mockPool) {
	pool := &mockPool{
		conn: &mockConn{},
	}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})
	handler := NewHandler(pool, engine, HandlerConfig{
		EndpointName: "imap-test",
		ConnectionConfig: ConnectionConfig{
			Host:     "imap.example.com",
			Port:     993,
			Username: "user",
			Password: "pass",
			TLS:      true,
		},
	})
	return handler, pool
}

func TestHandler_ListFolders(t *testing.T) {
	handler, _ := newTestHandler()

	req := httptest.NewRequest("GET", "/folders", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var folders []Folder
	if err := json.Unmarshal(rec.Body.Bytes(), &folders); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(folders) != 3 {
		t.Errorf("expected 3 folders, got %d", len(folders))
	}
}

func TestHandler_FetchMessages(t *testing.T) {
	handler, pool := newTestHandler()
	pool.conn = &mockConn{
		messages: []Message{
			{UID: 1, Subject: "Email 1", From: "a@example.com", Date: time.Now()},
			{UID: 2, Subject: "Email 2", From: "b@example.com", Date: time.Now()},
		},
	}

	req := httptest.NewRequest("GET", "/folders/inbox?limit=10", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var messages []Message
	if err := json.Unmarshal(rec.Body.Bytes(), &messages); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(messages) != 2 {
		t.Errorf("expected 2 messages, got %d", len(messages))
	}
}

func TestHandler_FetchMessagesWithSince(t *testing.T) {
	handler, pool := newTestHandler()
	pool.conn = &mockConn{
		messages: []Message{
			{UID: 1, Subject: "New email", From: "a@example.com", Date: time.Now()},
		},
	}

	req := httptest.NewRequest("GET", "/folders/inbox?since=2026-01-01", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	// Verify since parameter was passed
	if pool.conn.(*mockConn).lastOpts.Since == nil {
		t.Error("since parameter not passed to connection")
	}
}

func TestHandler_GetMessage(t *testing.T) {
	handler, _ := newTestHandler()

	req := httptest.NewRequest("GET", "/folders/inbox/messages/123", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var msg Message
	if err := json.Unmarshal(rec.Body.Bytes(), &msg); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if msg.UID != 123 {
		t.Errorf("expected UID 123, got %d", msg.UID)
	}
}

func TestHandler_MarkRead(t *testing.T) {
	handler, pool := newTestHandler()

	req := httptest.NewRequest("POST", "/folders/inbox/messages/123/mark-read", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	if pool.conn.(*mockConn).markedRead != 123 {
		t.Error("mark-read not called with correct UID")
	}
}

func TestHandler_MoveMessage(t *testing.T) {
	handler, pool := newTestHandler()

	req := httptest.NewRequest("POST", "/folders/inbox/messages/123/move?to=Archive", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	mc := pool.conn.(*mockConn)
	if mc.movedUID != 123 {
		t.Error("move not called with correct UID")
	}
	if mc.movedTo != "Archive" {
		t.Errorf("expected move to Archive, got %s", mc.movedTo)
	}
}

func TestHandler_MoveMessageMissingTo(t *testing.T) {
	handler, _ := newTestHandler()

	req := httptest.NewRequest("POST", "/folders/inbox/messages/123/move", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_PolicyDeny(t *testing.T) {
	pool := &mockPool{conn: &mockConn{}}
	engine := policy.New([]config.Rule{
		{Match: config.Match{Method: "POST"}, Action: "deny", Message: "writes not allowed"},
	})
	handler := NewHandler(pool, engine, HandlerConfig{
		EndpointName: "imap-test",
		ConnectionConfig: ConnectionConfig{
			Host: "imap.example.com",
			Port: 993,
		},
	})

	req := httptest.NewRequest("POST", "/message/123/mark-read", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403, got %d", rec.Code)
	}
}

func TestHandler_FolderFiltering(t *testing.T) {
	pool := &mockPool{conn: &mockConn{}}
	engine := policy.New([]config.Rule{
		{Match: config.Match{Path: "/folders/inbox*"}, Action: "allow"},
		{Match: config.Match{Path: "/*"}, Action: "deny", Message: "folder not allowed"},
	})
	handler := NewHandler(pool, engine, HandlerConfig{
		EndpointName: "imap-test",
		ConnectionConfig: ConnectionConfig{
			Host: "imap.example.com",
			Port: 993,
		},
	})

	// Inbox allowed
	req := httptest.NewRequest("GET", "/folders/inbox?limit=10", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("inbox should be allowed, got %d", rec.Code)
	}

	// Other folders denied
	req = httptest.NewRequest("GET", "/folders/drafts?limit=10", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("drafts should be denied, got %d", rec.Code)
	}
}

func TestHandler_InvalidUID(t *testing.T) {
	handler, _ := newTestHandler()

	req := httptest.NewRequest("GET", "/folders/inbox/messages/invalid", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", rec.Code)
	}
}

func TestHandler_ConnectionError(t *testing.T) {
	pool := &mockPool{failGet: true}
	engine := policy.New([]config.Rule{{Match: config.Match{Method: "*"}, Action: "allow"}})
	handler := NewHandler(pool, engine, HandlerConfig{
		EndpointName: "imap-test",
		ConnectionConfig: ConnectionConfig{
			Host: "imap.example.com",
			Port: 993,
		},
	})

	req := httptest.NewRequest("GET", "/folders", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("expected 502, got %d", rec.Code)
	}
}

func TestHandler_FolderWithSlash(t *testing.T) {
	handler, pool := newTestHandler()
	pool.conn = &mockConn{
		messages: []Message{
			{UID: 1, Subject: "Order confirmation", From: "shop@example.com", Date: time.Now()},
		},
	}

	// URL-encoded "Folder/Orders" -> "Folder%2FOrders"
	req := httptest.NewRequest("GET", "/folders/Folder%2FOrders", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the correct folder was passed to FetchMessages
	mc := pool.conn.(*mockConn)
	if mc.lastOpts.Folder != "Folder/Orders" {
		t.Errorf("expected folder 'Folder/Orders', got '%s'", mc.lastOpts.Folder)
	}
}

// Mock pool for handler tests
type mockPool struct {
	conn    Connection
	failGet bool
}

func (p *mockPool) Get(ctx context.Context, endpoint string, cfg ConnectionConfig) (Connection, error) {
	if p.failGet {
		return nil, ErrConnectionFailed
	}
	return p.conn, nil
}

func (p *mockPool) Put(endpoint string, conn Connection) {}
