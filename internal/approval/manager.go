package approval

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
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
	ID        string
	Token     string // Secret token for approving/denying
	Endpoint  string
	Method    string
	Path      string
	AgentID   string
	Status    Status
	CreatedAt time.Time
	ExpiresAt time.Time
	done      chan Status
}

// Manager handles approval requests.
type Manager struct {
	mu        sync.RWMutex
	requests  map[string]*Request
	baseURL   string
	timeout   time.Duration
	notifiers []notify.Channel
}

// NewManager creates a new approval manager.
func NewManager(baseURL string, timeout time.Duration) *Manager {
	return &Manager{
		requests: make(map[string]*Request),
		baseURL:  baseURL,
		timeout:  timeout,
	}
}

// AddNotifier adds a notification channel.
func (m *Manager) AddNotifier(n notify.Channel) {
	m.notifiers = append(m.notifiers, n)
}

// RequestApproval creates a new approval request and notifies.
// It blocks until approved, denied, or timeout.
func (m *Manager) RequestApproval(ctx context.Context, endpoint, method, path, agentID string) (bool, error) {
	// Generate request ID and token
	id := generateID()
	token := generateToken()

	req := &Request{
		ID:        id,
		Token:     token,
		Endpoint:  endpoint,
		Method:    method,
		Path:      path,
		AgentID:   agentID,
		Status:    Pending,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(m.timeout),
		done:      make(chan Status, 1),
	}

	m.mu.Lock()
	m.requests[id] = req
	m.mu.Unlock()

	// Send notifications
	msg := notify.Message{
		Title:      "Approval Required",
		Body:       fmt.Sprintf("Agent %s wants to %s %s on %s", agentID, method, path, endpoint),
		RequestID:  id,
		Endpoint:   endpoint,
		Method:     method,
		Path:       path,
		AgentID:    agentID,
		ApproveURL: fmt.Sprintf("%s/approve/%s?token=%s", m.baseURL, id, token),
		DenyURL:    fmt.Sprintf("%s/deny/%s?token=%s", m.baseURL, id, token),
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

// Approve marks a request as approved.
func (m *Manager) Approve(id, token string) error {
	return m.respond(id, token, Approved)
}

// Deny marks a request as denied.
func (m *Manager) Deny(id, token string) error {
	return m.respond(id, token, Denied)
}

func (m *Manager) respond(id, token string, status Status) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	req, ok := m.requests[id]
	if !ok {
		return fmt.Errorf("request not found")
	}

	if req.Token != token {
		return fmt.Errorf("invalid token")
	}

	if req.Status != Pending {
		return fmt.Errorf("request already %s", req.Status)
	}

	if time.Now().After(req.ExpiresAt) {
		req.Status = Expired
		return fmt.Errorf("request expired")
	}

	req.Status = status
	req.done <- status
	return nil
}

// Get returns a request by ID (for status checks).
func (m *Manager) Get(id string) (*Request, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	req, ok := m.requests[id]
	return req, ok
}

// GetPending returns a pending request's ID and token (for testing).
func (m *Manager) GetPending() (id, token string, found bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for id, req := range m.requests {
		if req.Status == Pending {
			return id, req.Token, true
		}
	}
	return "", "", false
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

// Handler returns an HTTP handler for approval endpoints.
func (m *Manager) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/approve/", m.handleApprove)
	mux.HandleFunc("/deny/", m.handleDeny)
	mux.HandleFunc("/status/", m.handleStatus)
	return mux
}

func (m *Manager) handleApprove(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/approve/"):]
	token := r.URL.Query().Get("token")

	if err := m.Approve(id, token); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>Approved</title></head>
<body style="font-family: system-ui; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #f0f9f0;">
<div style="text-align: center; padding: 2rem; background: white; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1);">
<h1 style="color: #22c55e; margin-bottom: 0.5rem;">Approved</h1>
<p style="color: #666;">Request %s has been approved.</p>
</div></body></html>`, id)
}

func (m *Manager) handleDeny(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/deny/"):]
	token := r.URL.Query().Get("token")

	if err := m.Deny(id, token); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!DOCTYPE html><html><head><meta charset="utf-8"><title>Denied</title></head>
<body style="font-family: system-ui; display: flex; justify-content: center; align-items: center; height: 100vh; margin: 0; background: #fef2f2;">
<div style="text-align: center; padding: 2rem; background: white; border-radius: 8px; box-shadow: 0 2px 10px rgba(0,0,0,0.1);">
<h1 style="color: #ef4444; margin-bottom: 0.5rem;">Denied</h1>
<p style="color: #666;">Request %s has been denied.</p>
</div></body></html>`, id)
}

func (m *Manager) handleStatus(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Path[len("/status/"):]

	req, ok := m.Get(id)
	if !ok {
		http.Error(w, "request not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"id":"%s","status":"%s"}`, req.ID, req.Status)
}

func generateID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func generateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
