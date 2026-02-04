package approval

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/wardgate/wardgate/internal/notify"
)

// Status represents the state of an approval request.
type Status int

const (
	Pending Status = iota
	Approved
	Denied
	Expired
)

func (s Status) String() string {
	switch s {
	case Pending:
		return "pending"
	case Approved:
		return "approved"
	case Denied:
		return "denied"
	case Expired:
		return "expired"
	default:
		return "unknown"
	}
}

// Request represents a pending approval request.
type Request struct {
	ID          string
	Endpoint    string
	Method      string
	Path        string
	AgentID     string
	Status      Status
	CreatedAt   time.Time
	ExpiresAt   time.Time
	RespondedAt time.Time // When approved/denied
	done        chan Status

	// Extended fields for content approval (Phase 7)
	ContentType string            // Type of content: "email", "api-call", etc.
	Summary     string            // Human-readable summary
	Body        string            // Full request body for review
	Headers     map[string]string // Relevant headers
}

// ApprovalRequest is the input for creating a new approval request.
type ApprovalRequest struct {
	Endpoint    string
	Method      string
	Path        string
	AgentID     string
	ContentType string
	Summary     string
	Body        string
	Headers     map[string]string
}

// Manager handles approval requests.
type Manager struct {
	mu           sync.RWMutex
	requests     map[string]*Request
	history      []*Request // Completed requests (approved/denied)
	historyLimit int        // Max history items to keep
	baseURL      string
	timeout      time.Duration
	notifiers    []notify.Channel
}

// NewManager creates a new approval manager.
func NewManager(baseURL string, timeout time.Duration) *Manager {
	return &Manager{
		requests:     make(map[string]*Request),
		history:      make([]*Request, 0),
		historyLimit: 100, // Default history limit
		baseURL:      baseURL,
		timeout:      timeout,
	}
}

// SetHistoryLimit sets the maximum number of history items to keep.
func (m *Manager) SetHistoryLimit(limit int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.historyLimit = limit
	// Trim if needed
	if len(m.history) > limit {
		m.history = m.history[len(m.history)-limit:]
	}
}

// AddNotifier adds a notification channel.
func (m *Manager) AddNotifier(n notify.Channel) {
	m.notifiers = append(m.notifiers, n)
}

// RequestApproval creates a new approval request and notifies.
// It blocks until approved, denied, or timeout.
func (m *Manager) RequestApproval(ctx context.Context, endpoint, method, path, agentID string) (bool, error) {
	return m.RequestApprovalWithContent(ctx, ApprovalRequest{
		Endpoint: endpoint,
		Method:   method,
		Path:     path,
		AgentID:  agentID,
	})
}

// RequestApprovalWithContent creates a new approval request with full content and notifies.
// It blocks until approved, denied, or timeout.
func (m *Manager) RequestApprovalWithContent(ctx context.Context, ar ApprovalRequest) (bool, error) {
	// Generate request ID
	id := generateID()

	req := &Request{
		ID:          id,
		Endpoint:    ar.Endpoint,
		Method:      ar.Method,
		Path:        ar.Path,
		AgentID:     ar.AgentID,
		Status:      Pending,
		CreatedAt:   time.Now(),
		ExpiresAt:   time.Now().Add(m.timeout),
		done:        make(chan Status, 1),
		ContentType: ar.ContentType,
		Summary:     ar.Summary,
		Body:        ar.Body,
		Headers:     ar.Headers,
	}

	m.mu.Lock()
	m.requests[id] = req
	m.mu.Unlock()

	// Build notification body
	body := fmt.Sprintf("Agent %s wants to %s %s on %s", ar.AgentID, ar.Method, ar.Path, ar.Endpoint)
	if ar.Summary != "" {
		body = ar.Summary
	}

	// Send notifications (link to Web UI dashboard)
	msg := notify.Message{
		Title:        "Approval Required",
		Body:         body,
		RequestID:    id,
		Endpoint:     ar.Endpoint,
		Method:       ar.Method,
		Path:         ar.Path,
		AgentID:      ar.AgentID,
		DashboardURL: fmt.Sprintf("%s/ui/", m.baseURL),
	}

	for _, n := range m.notifiers {
		go n.Send(ctx, msg)
	}

	// Wait for response or timeout
	select {
	case status := <-req.done:
		return status == Approved, nil
	case <-time.After(m.timeout):
		m.mu.Lock()
		if req.Status == Pending {
			req.Status = Expired
		}
		m.mu.Unlock()
		return false, fmt.Errorf("approval timeout")
	case <-ctx.Done():
		return false, ctx.Err()
	}
}

// respondByID responds to a request by ID (used by admin API).
func (m *Manager) respondByID(id string, status Status) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	req, ok := m.requests[id]
	if !ok {
		return fmt.Errorf("request not found")
	}

	if req.Status != Pending {
		return fmt.Errorf("request already %s", req.Status)
	}

	if time.Now().After(req.ExpiresAt) {
		req.Status = Expired
		return fmt.Errorf("request expired")
	}

	req.Status = status
	req.RespondedAt = time.Now()
	req.done <- status

	// Add to history
	m.addToHistory(req)

	return nil
}

// ApproveByID approves a request by ID (for admin API).
func (m *Manager) ApproveByID(id string) error {
	return m.respondByID(id, Approved)
}

// DenyByID denies a request by ID (for admin API).
func (m *Manager) DenyByID(id string) error {
	return m.respondByID(id, Denied)
}

// addToHistory adds a request to the history (must be called with lock held).
func (m *Manager) addToHistory(req *Request) {
	m.history = append(m.history, req)
	// Trim if over limit
	if len(m.history) > m.historyLimit {
		m.history = m.history[len(m.history)-m.historyLimit:]
	}
}

// List returns all pending approval requests.
func (m *Manager) List() []*Request {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var pending []*Request
	for _, req := range m.requests {
		if req.Status == Pending && time.Now().Before(req.ExpiresAt) {
			pending = append(pending, req)
		}
	}
	return pending
}

// History returns recent completed requests (newest first).
func (m *Manager) History(limit int) []*Request {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return in reverse order (newest first)
	result := make([]*Request, 0, limit)
	start := len(m.history) - 1
	for i := start; i >= 0 && len(result) < limit; i-- {
		result = append(result, m.history[i])
	}
	return result
}

// Get returns a request by ID (for status checks).
func (m *Manager) Get(id string) (*Request, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	req, ok := m.requests[id]
	return req, ok
}

// GetPending returns a pending request's ID (for testing).
func (m *Manager) GetPending() (id string, found bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, req := range m.requests {
		if req.Status == Pending {
			return id, true
		}
	}
	return "", false
}

// Cleanup removes expired requests older than the given duration.
func (m *Manager) Cleanup(maxAge time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for id, req := range m.requests {
		if req.CreatedAt.Before(cutoff) {
			delete(m.requests, id)
		}
	}
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}
