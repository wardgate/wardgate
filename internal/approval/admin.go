package approval

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wardgate/wardgate/internal/audit"
)

// RequestView is the JSON representation of a Request for the admin API.
// It omits the Token field for security.
type RequestView struct {
	ID          string            `json:"id"`
	Endpoint    string            `json:"endpoint"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	AgentID     string            `json:"agent_id,omitempty"`
	Status      string            `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	ExpiresAt   time.Time         `json:"expires_at"`
	RespondedAt *time.Time        `json:"responded_at,omitempty"`
	ContentType string            `json:"content_type,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	Body        string            `json:"body,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// toView converts a Request to a RequestView.
func toView(r *Request) RequestView {
	v := RequestView{
		ID:          r.ID,
		Endpoint:    r.Endpoint,
		Method:      r.Method,
		Path:        r.Path,
		AgentID:     r.AgentID,
		Status:      r.Status.String(),
		CreatedAt:   r.CreatedAt,
		ExpiresAt:   r.ExpiresAt,
		ContentType: r.ContentType,
		Summary:     r.Summary,
		Body:        r.Body,
		Headers:     r.Headers,
	}
	if !r.RespondedAt.IsZero() {
		v.RespondedAt = &r.RespondedAt
	}
	return v
}

// AdminHandler provides the admin API for managing approvals.
type AdminHandler struct {
	manager  *Manager
	adminKey string
	logStore *audit.Store
}

// NewAdminHandler creates a new admin handler.
func NewAdminHandler(manager *Manager, adminKey string) *AdminHandler {
	return &AdminHandler{
		manager:  manager,
		adminKey: adminKey,
	}
}

// SetLogStore sets the log store for the logs API.
func (h *AdminHandler) SetLogStore(store *audit.Store) {
	h.logStore = store
}

// ServeHTTP handles admin API requests.
func (h *AdminHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check admin authentication
	if !h.authenticate(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	path := r.URL.Path

	// Route requests
	switch {
	case path == "/ui/api/approvals" && r.Method == http.MethodGet:
		h.handleList(w, r)
	case path == "/ui/api/history" && r.Method == http.MethodGet:
		h.handleHistory(w, r)
	case path == "/ui/api/logs" && r.Method == http.MethodGet:
		h.handleLogs(w, r)
	case path == "/ui/api/logs/filters" && r.Method == http.MethodGet:
		h.handleLogsFilters(w, r)
	case strings.HasPrefix(path, "/ui/api/approvals/") && strings.HasSuffix(path, "/approve") && r.Method == http.MethodPost:
		h.handleApprove(w, r)
	case strings.HasPrefix(path, "/ui/api/approvals/") && strings.HasSuffix(path, "/deny") && r.Method == http.MethodPost:
		h.handleDeny(w, r)
	case strings.HasPrefix(path, "/ui/api/approvals/") && r.Method == http.MethodGet:
		h.handleGet(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *AdminHandler) authenticate(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return false
	}

	// Expect "Bearer <key>"
	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || parts[0] != "Bearer" {
		return false
	}

	return parts[1] == h.adminKey
}

func (h *AdminHandler) handleList(w http.ResponseWriter, r *http.Request) {
	pending := h.manager.List()

	views := make([]RequestView, len(pending))
	for i, req := range pending {
		views[i] = toView(req)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"approvals": views,
	})
}

func (h *AdminHandler) handleHistory(w http.ResponseWriter, r *http.Request) {
	history := h.manager.History(100)

	views := make([]RequestView, len(history))
	for i, req := range history {
		views[i] = toView(req)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"history": views,
	})
}

func (h *AdminHandler) handleGet(w http.ResponseWriter, r *http.Request) {
	// Extract ID from /ui/api/approvals/{id}
	path := r.URL.Path
	id := strings.TrimPrefix(path, "/ui/api/approvals/")

	req, ok := h.manager.Get(id)
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(toView(req))
}

func (h *AdminHandler) handleApprove(w http.ResponseWriter, r *http.Request) {
	// Extract ID from /ui/api/approvals/{id}/approve
	path := r.URL.Path
	id := strings.TrimPrefix(path, "/ui/api/approvals/")
	id = strings.TrimSuffix(id, "/approve")

	if err := h.manager.ApproveByID(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "approved",
	})
}

func (h *AdminHandler) handleDeny(w http.ResponseWriter, r *http.Request) {
	// Extract ID from /ui/api/approvals/{id}/deny
	path := r.URL.Path
	id := strings.TrimPrefix(path, "/ui/api/approvals/")
	id = strings.TrimSuffix(id, "/deny")

	if err := h.manager.DenyByID(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "denied",
	})
}

func (h *AdminHandler) handleLogs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()

	params := audit.QueryParams{
		Endpoint: query.Get("endpoint"),
		AgentID:  query.Get("agent"),
		Decision: query.Get("decision"),
		Method:   query.Get("method"),
		Limit:    50,
	}

	if limitStr := query.Get("limit"); limitStr != "" {
		if limit, err := strconv.Atoi(limitStr); err == nil && limit > 0 {
			params.Limit = limit
		}
	}

	if beforeStr := query.Get("before"); beforeStr != "" {
		if before, err := time.Parse(time.RFC3339, beforeStr); err == nil {
			params.Before = before
		}
	}

	var logs []audit.StoredEntry
	if h.logStore != nil {
		logs = h.logStore.Query(params)
	} else {
		logs = []audit.StoredEntry{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"logs": logs,
	})
}

func (h *AdminHandler) handleLogsFilters(w http.ResponseWriter, r *http.Request) {
	var endpoints, agents []string

	if h.logStore != nil {
		endpoints = h.logStore.GetEndpoints()
		agents = h.logStore.GetAgents()
	} else {
		endpoints = []string{}
		agents = []string{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"endpoints": endpoints,
		"agents":    agents,
		"decisions": []string{"allow", "deny", "rate_limited", "error"},
	})
}
