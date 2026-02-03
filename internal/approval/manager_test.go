package approval

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestManager_ApproveFlow(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)

	// Start approval request in goroutine
	done := make(chan bool)
	var approved bool
	var err error

	go func() {
		approved, err = m.RequestApproval(context.Background(), "test-api", "POST", "/tasks", "agent-1")
		done <- true
	}()

	// Wait a bit for request to be created
	time.Sleep(50 * time.Millisecond)

	// Find the pending request
	var reqID, token string
	m.mu.RLock()
	for id, req := range m.requests {
		if req.Status == Pending {
			reqID = id
			token = req.Token
			break
		}
	}
	m.mu.RUnlock()

	if reqID == "" {
		t.Fatal("no pending request found")
	}

	// Approve it
	if err := m.Approve(reqID, token); err != nil {
		t.Fatalf("approve failed: %v", err)
	}

	// Wait for result
	<-done

	if err != nil {
		t.Fatalf("RequestApproval error: %v", err)
	}
	if !approved {
		t.Error("expected approved=true")
	}
}

func TestManager_DenyFlow(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)

	done := make(chan bool)
	var approved bool

	go func() {
		approved, _ = m.RequestApproval(context.Background(), "test-api", "DELETE", "/tasks/123", "agent-1")
		done <- true
	}()

	time.Sleep(50 * time.Millisecond)

	var reqID, token string
	m.mu.RLock()
	for id, req := range m.requests {
		if req.Status == Pending {
			reqID = id
			token = req.Token
			break
		}
	}
	m.mu.RUnlock()

	if err := m.Deny(reqID, token); err != nil {
		t.Fatalf("deny failed: %v", err)
	}

	<-done

	if approved {
		t.Error("expected approved=false after deny")
	}
}

func TestManager_InvalidToken(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)

	go m.RequestApproval(context.Background(), "test-api", "GET", "/tasks", "agent-1")
	time.Sleep(50 * time.Millisecond)

	var reqID string
	m.mu.RLock()
	for id := range m.requests {
		reqID = id
		break
	}
	m.mu.RUnlock()

	err := m.Approve(reqID, "wrong-token")
	if err == nil {
		t.Error("expected error for invalid token")
	}
}

func TestManager_Timeout(t *testing.T) {
	m := NewManager("http://localhost:8080", 100*time.Millisecond)

	approved, err := m.RequestApproval(context.Background(), "test-api", "GET", "/tasks", "agent-1")

	if err == nil {
		t.Error("expected timeout error")
	}
	if approved {
		t.Error("expected approved=false on timeout")
	}
}

func TestManager_Handler_Approve(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)

	go m.RequestApproval(context.Background(), "test-api", "GET", "/tasks", "agent-1")
	time.Sleep(50 * time.Millisecond)

	var reqID, token string
	m.mu.RLock()
	for id, req := range m.requests {
		reqID = id
		token = req.Token
		break
	}
	m.mu.RUnlock()

	handler := m.Handler()
	req := httptest.NewRequest("GET", "/approve/"+reqID+"?token="+token, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	// Check status
	r, _ := m.Get(reqID)
	if r.Status != Approved {
		t.Errorf("expected Approved status, got %v", r.Status)
	}
}

func TestManager_Handler_Status(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)

	go m.RequestApproval(context.Background(), "test-api", "GET", "/tasks", "agent-1")
	time.Sleep(50 * time.Millisecond)

	var reqID string
	m.mu.RLock()
	for id := range m.requests {
		reqID = id
		break
	}
	m.mu.RUnlock()

	handler := m.Handler()
	req := httptest.NewRequest("GET", "/status/"+reqID, nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	body := rec.Body.String()
	if body == "" {
		t.Error("expected non-empty body")
	}
}

func TestManager_Cleanup(t *testing.T) {
	m := NewManager("http://localhost:8080", 100*time.Millisecond)

	// Create and let timeout
	m.RequestApproval(context.Background(), "test-api", "GET", "/tasks", "agent-1")

	// Should have one request
	m.mu.RLock()
	count := len(m.requests)
	m.mu.RUnlock()
	if count != 1 {
		t.Errorf("expected 1 request, got %d", count)
	}

	// Cleanup old requests
	m.Cleanup(0)

	m.mu.RLock()
	count = len(m.requests)
	m.mu.RUnlock()
	if count != 0 {
		t.Errorf("expected 0 requests after cleanup, got %d", count)
	}
}
