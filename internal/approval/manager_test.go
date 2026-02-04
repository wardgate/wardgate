package approval

import (
	"context"
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
	reqID, found := m.GetPending()
	if !found {
		t.Fatal("no pending request found")
	}

	// Approve it via admin API
	if err := m.ApproveByID(reqID); err != nil {
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

	reqID, found := m.GetPending()
	if !found {
		t.Fatal("no pending request found")
	}

	if err := m.DenyByID(reqID); err != nil {
		t.Fatalf("deny failed: %v", err)
	}

	<-done

	if approved {
		t.Error("expected approved=false after deny")
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

// --- Phase 7: Extended Request and Admin API Tests ---

func TestManager_RequestApprovalWithContent(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)

	// Start approval request with content
	done := make(chan bool)
	go func() {
		m.RequestApprovalWithContent(context.Background(), ApprovalRequest{
			Endpoint:    "smtp-personal",
			Method:      "POST",
			Path:        "/send",
			AgentID:     "agent-1",
			ContentType: "email",
			Summary:     "Email to user@example.com: Test Subject",
			Body:        `{"to":["user@example.com"],"subject":"Test Subject","body":"Hello world"}`,
			Headers:     map[string]string{"Content-Type": "application/json"},
		})
		done <- true
	}()

	time.Sleep(50 * time.Millisecond)

	// Find the pending request and verify content
	id, found := m.GetPending()
	if !found {
		t.Fatal("no pending request found")
	}

	req, ok := m.Get(id)
	if !ok {
		t.Fatal("request not found by ID")
	}

	if req.ContentType != "email" {
		t.Errorf("expected ContentType=email, got %s", req.ContentType)
	}
	if req.Summary != "Email to user@example.com: Test Subject" {
		t.Errorf("unexpected Summary: %s", req.Summary)
	}
	if req.Body == "" {
		t.Error("expected Body to be set")
	}
	if req.Headers["Content-Type"] != "application/json" {
		t.Error("expected Content-Type header to be set")
	}

	// Approve to unblock goroutine
	m.ApproveByID(id)
	<-done
}

func TestManager_List(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)

	// Create multiple pending requests
	for i := 0; i < 3; i++ {
		go func(n int) {
			m.RequestApprovalWithContent(context.Background(), ApprovalRequest{
				Endpoint: "test-api",
				Method:   "POST",
				Path:     "/tasks",
				AgentID:  "agent-1",
				Summary:  "Request " + string(rune('A'+n)),
			})
		}(i)
	}

	time.Sleep(100 * time.Millisecond)

	// List pending requests
	pending := m.List()
	if len(pending) != 3 {
		t.Errorf("expected 3 pending requests, got %d", len(pending))
	}

	// Approve one
	if len(pending) > 0 {
		m.ApproveByID(pending[0].ID)
	}

	time.Sleep(50 * time.Millisecond)

	// List again - should have 2 pending
	pending = m.List()
	if len(pending) != 2 {
		t.Errorf("expected 2 pending requests after approve, got %d", len(pending))
	}
}

func TestManager_History(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)

	// Create and approve a request
	done := make(chan bool)
	go func() {
		m.RequestApprovalWithContent(context.Background(), ApprovalRequest{
			Endpoint: "test-api",
			Method:   "POST",
			Path:     "/tasks",
			AgentID:  "agent-1",
		})
		done <- true
	}()

	time.Sleep(50 * time.Millisecond)
	id, _ := m.GetPending()
	m.ApproveByID(id)
	<-done

	// Create and deny a request
	go func() {
		m.RequestApprovalWithContent(context.Background(), ApprovalRequest{
			Endpoint: "test-api",
			Method:   "DELETE",
			Path:     "/tasks/123",
			AgentID:  "agent-1",
		})
		done <- true
	}()

	time.Sleep(50 * time.Millisecond)
	id, _ = m.GetPending()
	m.DenyByID(id)
	<-done

	// Check history
	history := m.History(10)
	if len(history) != 2 {
		t.Errorf("expected 2 history items, got %d", len(history))
	}

	// History should be in reverse chronological order (newest first)
	if len(history) >= 2 {
		if history[0].Status != Denied {
			t.Errorf("expected first history item to be Denied, got %v", history[0].Status)
		}
		if history[1].Status != Approved {
			t.Errorf("expected second history item to be Approved, got %v", history[1].Status)
		}
	}
}

func TestManager_HistoryLimit(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)
	m.SetHistoryLimit(5) // Keep only 5 items

	// Create and approve 10 requests
	for i := 0; i < 10; i++ {
		done := make(chan bool)
		go func() {
			m.RequestApprovalWithContent(context.Background(), ApprovalRequest{
				Endpoint: "test-api",
				Method:   "GET",
				Path:     "/tasks",
				AgentID:  "agent-1",
			})
			done <- true
		}()

		time.Sleep(30 * time.Millisecond)
		id, _ := m.GetPending()
		m.ApproveByID(id)
		<-done
	}

	// History should be capped at 5
	history := m.History(100)
	if len(history) != 5 {
		t.Errorf("expected history to be capped at 5, got %d", len(history))
	}
}

func TestManager_GetByID(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)

	go func() {
		m.RequestApprovalWithContent(context.Background(), ApprovalRequest{
			Endpoint:    "smtp-personal",
			Method:      "POST",
			Path:        "/send",
			AgentID:     "agent-1",
			ContentType: "email",
			Summary:     "Test email",
			Body:        `{"to":["test@example.com"]}`,
		})
	}()

	time.Sleep(50 * time.Millisecond)
	id, _ := m.GetPending()

	// Get by ID should return full details including body
	req, ok := m.Get(id)
	if !ok {
		t.Fatal("request not found")
	}

	if req.Body == "" {
		t.Error("expected Body to be included in Get response")
	}
	if req.ContentType != "email" {
		t.Error("expected ContentType to be included")
	}
}
