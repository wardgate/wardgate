package approval

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/wardgate/wardgate/internal/audit"
	"github.com/wardgate/wardgate/internal/grants"
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
	manager    *Manager
	adminKey   string
	logStore   *audit.Store
	grantStore *grants.Store
}

// SetGrantStore sets the grant store for creating grants on approval.
func (h *AdminHandler) SetGrantStore(s *grants.Store) {
	h.grantStore = s
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
	case path == "/ui/api/grants" && r.Method == http.MethodGet:
		h.handleGrantsList(w, r)
	case path == "/ui/api/grants" && r.Method == http.MethodPost:
		h.handleGrantsAdd(w, r)
	case strings.HasPrefix(path, "/ui/api/grants/") && r.Method == http.MethodDelete:
		h.handleGrantsRevoke(w, r)
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

	// Capture the request before approving (it may be removed after approval)
	approvalReq, _ := h.manager.Get(id)

	if err := h.manager.ApproveByID(id); err != nil {
		if strings.Contains(err.Error(), "not found") {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Handle grant creation if requested
	grantParam := r.URL.Query().Get("grant")
	if grantParam != "" && h.grantStore != nil && approvalReq != nil {
		if err := h.createGrant(grantParam, approvalReq); err != nil {
			// Grant creation failed, but approval succeeded -- log and continue
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]string{
				"status":        "approved",
				"grant_error":   err.Error(),
			})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "approved",
	})
}

func (h *AdminHandler) createGrant(grantParam string, req *Request) error {
	var expiresAt *time.Time

	switch grantParam {
	case "always":
		// permanent, nil expiry
	default:
		// Parse as duration
		d, err := time.ParseDuration(grantParam)
		if err != nil {
			return fmt.Errorf("invalid grant duration %q: %w", grantParam, err)
		}
		exp := time.Now().Add(d)
		expiresAt = &exp
	}

	g := grants.Grant{
		AgentID:   req.AgentID,
		Scope:     req.Endpoint,
		Action:    "allow",
		Reason:    fmt.Sprintf("approved via UI (grant=%s)", grantParam),
		ExpiresAt: expiresAt,
	}

	// Set match fields based on scope type
	if strings.HasPrefix(req.Endpoint, "conclave:") {
		g.Match = grants.GrantMatch{Command: req.Path}
	} else {
		g.Match = grants.GrantMatch{Method: req.Method, Path: req.Path}
	}

	h.grantStore.Add(g)
	return nil
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

func (h *AdminHandler) handleGrantsList(w http.ResponseWriter, r *http.Request) {
	if h.grantStore == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"grants": []interface{}{}})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"grants": h.grantStore.List(),
	})
}

func (h *AdminHandler) handleGrantsAdd(w http.ResponseWriter, r *http.Request) {
	if h.grantStore == nil {
		http.Error(w, "grants not configured", http.StatusServiceUnavailable)
		return
	}

	var g grants.Grant
	if err := json.NewDecoder(r.Body).Decode(&g); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	added := h.grantStore.Add(g)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(added)
}

func (h *AdminHandler) handleGrantsRevoke(w http.ResponseWriter, r *http.Request) {
	if h.grantStore == nil {
		http.Error(w, "grants not configured", http.StatusServiceUnavailable)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/ui/api/grants/")
	if err := h.grantStore.Revoke(id); err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "revoked"})
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
