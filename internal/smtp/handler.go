package smtp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/wardgate/wardgate/internal/approval"
	"github.com/wardgate/wardgate/internal/policy"
)

// HandlerConfig configures the SMTP handler.
type HandlerConfig struct {
	EndpointName      string
	From              string   // Default from address
	AllowedRecipients []string // Allowlist of recipients (email or @domain)
	KnownRecipients   []string // Known recipients that don't need approval
	AskNewRecipients  bool     // Ask before sending to new recipients
	BlockedKeywords   []string // Keywords to block in subject/body
}

// Handler handles REST requests for SMTP operations.
type Handler struct {
	client    Client
	engine    *policy.Engine
	config    HandlerConfig
	approvals ApprovalRequester
}

// NewHandler creates a new SMTP REST handler.
func NewHandler(client Client, engine *policy.Engine, cfg HandlerConfig) *Handler {
	return &Handler{
		client: client,
		engine: engine,
		config: cfg,
	}
}

// SetApprovalManager sets the approval manager for ask workflows.
func (h *Handler) SetApprovalManager(m ApprovalRequester) {
	h.approvals = m
}

// ServeHTTP handles incoming REST requests and routes them to SMTP operations.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get agent ID from header
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		agentID = r.RemoteAddr
	}

	// Route based on path
	path := strings.TrimPrefix(r.URL.Path, "/")

	switch {
	case path == "send" && r.Method == "POST":
		h.handleSend(w, r, agentID)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *Handler) handleSend(w http.ResponseWriter, r *http.Request, agentID string) {
	// Evaluate policy first
	decision := h.engine.EvaluateWithKey(r.Method, r.URL.Path, agentID)
	if decision.Action == policy.Deny {
		http.Error(w, decision.Message, http.StatusForbidden)
		return
	}
	if decision.Action == policy.RateLimited {
		w.Header().Set("Retry-After", "60")
		http.Error(w, decision.Message, http.StatusTooManyRequests)
		return
	}

	// Parse request body
	var req SendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if len(req.To) == 0 {
		http.Error(w, "at least one recipient required", http.StatusBadRequest)
		return
	}

	// Build email from request
	email := Email{
		From:     h.config.From,
		To:       req.To,
		Cc:       req.Cc,
		Bcc:      req.Bcc,
		ReplyTo:  req.ReplyTo,
		Subject:  req.Subject,
		Body:     req.Body,
		HTMLBody: req.HTMLBody,
	}

	// Check recipient allowlist
	if len(h.config.AllowedRecipients) > 0 {
		allRecipients := append(append(email.To, email.Cc...), email.Bcc...)
		for _, rcpt := range allRecipients {
			if !h.isRecipientAllowed(rcpt) {
				http.Error(w, fmt.Sprintf("recipient not allowed: %s", rcpt), http.StatusForbidden)
				return
			}
		}
	}

	// Check content filtering
	if len(h.config.BlockedKeywords) > 0 {
		if h.containsBlockedKeyword(email.Subject) || h.containsBlockedKeyword(email.Body) {
			http.Error(w, "email blocked by content filter", http.StatusForbidden)
			return
		}
	}

	// Check if approval is needed
	needsApproval := decision.Action == policy.Ask

	// Check for new recipients if configured
	if h.config.AskNewRecipients && !needsApproval {
		allRecipients := append(append(email.To, email.Cc...), email.Bcc...)
		for _, rcpt := range allRecipients {
			if !h.isKnownRecipient(rcpt) {
				needsApproval = true
				break
			}
		}
	}

	// Handle approval workflow
	if needsApproval {
		if h.approvals == nil {
			http.Error(w, "ask action requires approval manager configuration", http.StatusServiceUnavailable)
			return
		}

		// Build approval request with full email content for review
		emailJSON, _ := json.Marshal(req)
		summary := fmt.Sprintf("Email to %s: %s", strings.Join(email.To, ", "), email.Subject)

		approved, err := h.approvals.RequestApprovalWithContent(r.Context(), approval.ApprovalRequest{
			Endpoint:    h.config.EndpointName,
			Method:      r.Method,
			Path:        "/send",
			AgentID:     agentID,
			ContentType: "email",
			Summary:     summary,
			Body:        string(emailJSON),
			Headers:     map[string]string{"Content-Type": "application/json"},
		})
		if err != nil {
			http.Error(w, fmt.Sprintf("approval failed: %v", err), http.StatusForbidden)
			return
		}
		if !approved {
			http.Error(w, "request denied by approver", http.StatusForbidden)
			return
		}
	}

	// Send email
	if err := h.client.Send(r.Context(), email); err != nil {
		fmt.Fprintf(os.Stderr, "SMTP send error for %s: %v\n", h.config.EndpointName, err)
		http.Error(w, "failed to send email", http.StatusBadGateway)
		return
	}

	// Return success response
	h.writeJSON(w, SendResponse{
		Status: "sent",
	})
}

func (h *Handler) isRecipientAllowed(email string) bool {
	email = strings.ToLower(email)
	for _, allowed := range h.config.AllowedRecipients {
		allowed = strings.ToLower(allowed)
		if strings.HasPrefix(allowed, "@") {
			// Domain match
			if strings.HasSuffix(email, allowed) {
				return true
			}
		} else {
			// Exact match
			if email == allowed {
				return true
			}
		}
	}
	return false
}

func (h *Handler) isKnownRecipient(email string) bool {
	email = strings.ToLower(email)
	for _, known := range h.config.KnownRecipients {
		known = strings.ToLower(known)
		if strings.HasPrefix(known, "@") {
			// Domain match
			if strings.HasSuffix(email, known) {
				return true
			}
		} else {
			// Exact match
			if email == known {
				return true
			}
		}
	}
	return false
}

func (h *Handler) containsBlockedKeyword(text string) bool {
	textLower := strings.ToLower(text)
	for _, keyword := range h.config.BlockedKeywords {
		if strings.Contains(textLower, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

func (h *Handler) writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
