package approval

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wardgate/wardgate/internal/audit"
)

func TestAdminHandler_Unauthorized(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)
	h := NewAdminHandler(m, "secret-admin-key")

	// No auth header
	req := httptest.NewRequest("GET", "/ui/api/approvals", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}

	// Wrong auth header
	req = httptest.NewRequest("GET", "/ui/api/approvals", nil)
	req.Header.Set("Authorization", "Bearer wrong-key")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong key, got %d", rec.Code)
	}
}

func TestAdminHandler_ListApprovals(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)
	h := NewAdminHandler(m, "secret-admin-key")

	// Create some pending requests
	for i := 0; i < 2; i++ {
		go func(n int) {
			m.RequestApprovalWithContent(context.Background(), ApprovalRequest{
				Endpoint: "test-api",
				Method:   "POST",
				Path:     "/tasks",
				AgentID:  "agent-1",
				Summary:  "Test request",
			})
		}(i)
	}

	time.Sleep(100 * time.Millisecond)

	req := httptest.NewRequest("GET", "/ui/api/approvals", nil)
	req.Header.Set("Authorization", "Bearer secret-admin-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Approvals []RequestView `json:"approvals"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(response.Approvals) != 2 {
		t.Errorf("expected 2 approvals, got %d", len(response.Approvals))
	}

	// Verify fields are present (but token should NOT be exposed)
	if len(response.Approvals) > 0 {
		a := response.Approvals[0]
		if a.ID == "" {
			t.Error("expected ID to be set")
		}
		if a.Endpoint == "" {
			t.Error("expected Endpoint to be set")
		}
		// Token should NOT be in the response for security
		// (admin uses ID-based approve, not token-based)
	}
}

func TestAdminHandler_GetApproval(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)
	h := NewAdminHandler(m, "secret-admin-key")

	// Create a pending request with body
	go func() {
		m.RequestApprovalWithContent(context.Background(), ApprovalRequest{
			Endpoint:    "smtp-personal",
			Method:      "POST",
			Path:        "/send",
			AgentID:     "agent-1",
			ContentType: "email",
			Summary:     "Email to test@example.com",
			Body:        `{"to":["test@example.com"],"subject":"Hello"}`,
		})
	}()

	time.Sleep(50 * time.Millisecond)
	id, _ := m.GetPending()

	req := httptest.NewRequest("GET", "/ui/api/approvals/"+id, nil)
	req.Header.Set("Authorization", "Bearer secret-admin-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response RequestView
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if response.ID != id {
		t.Errorf("expected ID=%s, got %s", id, response.ID)
	}
	if response.Body == "" {
		t.Error("expected Body to be included for detail view")
	}
	if response.ContentType != "email" {
		t.Error("expected ContentType to be included")
	}
}

func TestAdminHandler_ApproveByID(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)
	h := NewAdminHandler(m, "secret-admin-key")

	// Create a pending request
	done := make(chan bool)
	var approved bool

	go func() {
		approved, _ = m.RequestApprovalWithContent(context.Background(), ApprovalRequest{
			Endpoint: "test-api",
			Method:   "POST",
			Path:     "/tasks",
			AgentID:  "agent-1",
		})
		done <- true
	}()

	time.Sleep(50 * time.Millisecond)
	id, _ := m.GetPending()

	// Approve via admin API
	req := httptest.NewRequest("POST", "/ui/api/approvals/"+id+"/approve", nil)
	req.Header.Set("Authorization", "Bearer secret-admin-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	<-done

	if !approved {
		t.Error("expected request to be approved")
	}
}

func TestAdminHandler_DenyByID(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)
	h := NewAdminHandler(m, "secret-admin-key")

	done := make(chan bool)
	var approved bool

	go func() {
		approved, _ = m.RequestApprovalWithContent(context.Background(), ApprovalRequest{
			Endpoint: "test-api",
			Method:   "DELETE",
			Path:     "/tasks/123",
			AgentID:  "agent-1",
		})
		done <- true
	}()

	time.Sleep(50 * time.Millisecond)
	id, _ := m.GetPending()

	req := httptest.NewRequest("POST", "/ui/api/approvals/"+id+"/deny", nil)
	req.Header.Set("Authorization", "Bearer secret-admin-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	<-done

	if approved {
		t.Error("expected request to be denied")
	}
}

func TestAdminHandler_History(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)
	h := NewAdminHandler(m, "secret-admin-key")

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

	// Get history via admin API
	req := httptest.NewRequest("GET", "/ui/api/history", nil)
	req.Header.Set("Authorization", "Bearer secret-admin-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		History []RequestView `json:"history"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(response.History) != 1 {
		t.Errorf("expected 1 history item, got %d", len(response.History))
	}

	if len(response.History) > 0 && response.History[0].Status != "approved" {
		t.Errorf("expected status=approved, got %s", response.History[0].Status)
	}
}

func TestAdminHandler_NotFound(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)
	h := NewAdminHandler(m, "secret-admin-key")

	req := httptest.NewRequest("GET", "/ui/api/approvals/nonexistent", nil)
	req.Header.Set("Authorization", "Bearer secret-admin-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestAdminHandler_ApproveNotFound(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)
	h := NewAdminHandler(m, "secret-admin-key")

	req := httptest.NewRequest("POST", "/ui/api/approvals/nonexistent/approve", nil)
	req.Header.Set("Authorization", "Bearer secret-admin-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestAdminHandler_LogsEndpoint(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)
	store := audit.NewStore(100)
	h := NewAdminHandler(m, "secret-admin-key")
	h.SetLogStore(store)

	// Add some log entries
	store.Add(audit.StoredEntry{
		Entry:     audit.Entry{RequestID: "req1", Endpoint: "todoist", Method: "GET", Path: "/tasks", Decision: "allow"},
		Timestamp: time.Now(),
	})
	store.Add(audit.StoredEntry{
		Entry:     audit.Entry{RequestID: "req2", Endpoint: "github", Method: "POST", Path: "/issues", Decision: "allow"},
		Timestamp: time.Now(),
	})

	req := httptest.NewRequest("GET", "/ui/api/logs", nil)
	req.Header.Set("Authorization", "Bearer secret-admin-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Logs []audit.StoredEntry `json:"logs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if len(response.Logs) != 2 {
		t.Errorf("expected 2 logs, got %d", len(response.Logs))
	}
}

func TestAdminHandler_LogsWithFilters(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)
	store := audit.NewStore(100)
	h := NewAdminHandler(m, "secret-admin-key")
	h.SetLogStore(store)

	// Add log entries
	store.Add(audit.StoredEntry{
		Entry:     audit.Entry{RequestID: "req1", Endpoint: "todoist", AgentID: "agent1", Decision: "allow"},
		Timestamp: time.Now(),
	})
	store.Add(audit.StoredEntry{
		Entry:     audit.Entry{RequestID: "req2", Endpoint: "github", AgentID: "agent1", Decision: "deny"},
		Timestamp: time.Now(),
	})
	store.Add(audit.StoredEntry{
		Entry:     audit.Entry{RequestID: "req3", Endpoint: "todoist", AgentID: "agent2", Decision: "allow"},
		Timestamp: time.Now(),
	})

	// Filter by endpoint
	req := httptest.NewRequest("GET", "/ui/api/logs?endpoint=todoist", nil)
	req.Header.Set("Authorization", "Bearer secret-admin-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	var response struct {
		Logs []audit.StoredEntry `json:"logs"`
	}
	json.Unmarshal(rec.Body.Bytes(), &response)

	if len(response.Logs) != 2 {
		t.Errorf("expected 2 todoist logs, got %d", len(response.Logs))
	}

	// Filter by decision
	req = httptest.NewRequest("GET", "/ui/api/logs?decision=deny", nil)
	req.Header.Set("Authorization", "Bearer secret-admin-key")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	json.Unmarshal(rec.Body.Bytes(), &response)

	if len(response.Logs) != 1 {
		t.Errorf("expected 1 deny log, got %d", len(response.Logs))
	}
}

func TestAdminHandler_LogsFiltersMetadata(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)
	store := audit.NewStore(100)
	h := NewAdminHandler(m, "secret-admin-key")
	h.SetLogStore(store)

	store.Add(audit.StoredEntry{
		Entry:     audit.Entry{Endpoint: "todoist", AgentID: "agent1"},
		Timestamp: time.Now(),
	})
	store.Add(audit.StoredEntry{
		Entry:     audit.Entry{Endpoint: "github", AgentID: "agent2"},
		Timestamp: time.Now(),
	})

	req := httptest.NewRequest("GET", "/ui/api/logs/filters", nil)
	req.Header.Set("Authorization", "Bearer secret-admin-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var response struct {
		Endpoints []string `json:"endpoints"`
		Agents    []string `json:"agents"`
	}
	json.Unmarshal(rec.Body.Bytes(), &response)

	if len(response.Endpoints) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(response.Endpoints))
	}
	if len(response.Agents) != 2 {
		t.Errorf("expected 2 agents, got %d", len(response.Agents))
	}
}

func TestAdminHandler_LogsNoStore(t *testing.T) {
	m := NewManager("http://localhost:8080", 5*time.Second)
	h := NewAdminHandler(m, "secret-admin-key")
	// No log store set

	req := httptest.NewRequest("GET", "/ui/api/logs", nil)
	req.Header.Set("Authorization", "Bearer secret-admin-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}

	var response struct {
		Logs []audit.StoredEntry `json:"logs"`
	}
	json.Unmarshal(rec.Body.Bytes(), &response)

	if len(response.Logs) != 0 {
		t.Errorf("expected 0 logs when no store, got %d", len(response.Logs))
	}
}
