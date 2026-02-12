package exec

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/wardgate/wardgate/internal/approval"
	"github.com/wardgate/wardgate/internal/policy"
)

// EvaluateRequest is the JSON body for POST /exec/evaluate.
type EvaluateRequest struct {
	Command string `json:"command"`       // Resolved absolute path of the executable
	Args    string `json:"args"`          // Joined argument string
	Cwd     string `json:"cwd"`           // Absolute working directory
	AgentID string `json:"agent_id"`      // Agent identifier
	Raw     string `json:"raw,omitempty"` // Original command string (for display)
}

// EvaluateResponse is the JSON response for POST /exec/evaluate.
type EvaluateResponse struct {
	Action     string `json:"action"`                // "allow", "deny", "ask", "rate_limited"
	Message    string `json:"message,omitempty"`     // Human-readable message
	ApprovalID string `json:"approval_id,omitempty"` // Set when action is "ask"
}

// ReportRequest is the JSON body for POST /exec/report.
type ReportRequest struct {
	Command    string `json:"command"`
	Args       string `json:"args"`
	Cwd        string `json:"cwd"`
	AgentID    string `json:"agent_id"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// Handler handles exec evaluation and reporting requests.
type Handler struct {
	engine      *policy.Engine
	approvalMgr *approval.Manager
	endpoint    string
}

// NewHandler creates a new exec handler.
func NewHandler(engine *policy.Engine, endpoint string) *Handler {
	return &Handler{
		engine:   engine,
		endpoint: endpoint,
	}
}

// SetApprovalManager sets the approval manager for "ask" decisions.
func (h *Handler) SetApprovalManager(mgr *approval.Manager) {
	h.approvalMgr = mgr
}

// ServeHTTP routes exec requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/evaluate":
		h.handleEvaluate(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/report":
		h.handleReport(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) handleEvaluate(w http.ResponseWriter, r *http.Request) {
	var req EvaluateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, EvaluateResponse{
			Action:  "deny",
			Message: "invalid request body",
		})
		return
	}

	if req.Command == "" {
		writeJSON(w, http.StatusBadRequest, EvaluateResponse{
			Action:  "deny",
			Message: "command is required",
		})
		return
	}

	agentID := req.AgentID
	if agentID == "" {
		agentID = r.Header.Get("X-Agent-ID")
	}

	decision := h.engine.EvaluateExec(req.Command, req.Args, req.Cwd, agentID)

	switch decision.Action {
	case policy.Allow:
		writeJSON(w, http.StatusOK, EvaluateResponse{
			Action: "allow",
		})

	case policy.Deny:
		writeJSON(w, http.StatusForbidden, EvaluateResponse{
			Action:  "deny",
			Message: decision.Message,
		})

	case policy.RateLimited:
		writeJSON(w, http.StatusTooManyRequests, EvaluateResponse{
			Action:  "rate_limited",
			Message: decision.Message,
		})

	case policy.Ask:
		if h.approvalMgr == nil {
			writeJSON(w, http.StatusForbidden, EvaluateResponse{
				Action:  "deny",
				Message: "approval required but no approval manager configured",
			})
			return
		}

		// Build summary for the approval UI
		summary := fmt.Sprintf("Agent %s wants to execute: %s", agentID, req.Command)
		if req.Args != "" {
			summary = fmt.Sprintf("Agent %s wants to execute: %s %s", agentID, req.Command, req.Args)
		}

		displayCmd := req.Raw
		if displayCmd == "" {
			displayCmd = req.Command
			if req.Args != "" {
				displayCmd = req.Command + " " + req.Args
			}
		}

		approved, err := h.approvalMgr.RequestApprovalWithContent(r.Context(), approval.ApprovalRequest{
			Endpoint:    h.endpoint,
			Method:      "EXEC",
			Path:        req.Command,
			AgentID:     agentID,
			ContentType: "exec",
			Summary:     summary,
			Body:        displayCmd,
			Headers: map[string]string{
				"Command": req.Command,
				"Args":    req.Args,
				"Cwd":     req.Cwd,
			},
		})

		if err != nil {
			writeJSON(w, http.StatusGatewayTimeout, EvaluateResponse{
				Action:  "deny",
				Message: fmt.Sprintf("approval timeout: %v", err),
			})
			return
		}

		if approved {
			writeJSON(w, http.StatusOK, EvaluateResponse{
				Action: "allow",
			})
		} else {
			writeJSON(w, http.StatusForbidden, EvaluateResponse{
				Action:  "deny",
				Message: "execution denied by approver",
			})
		}

	default:
		writeJSON(w, http.StatusForbidden, EvaluateResponse{
			Action:  "deny",
			Message: decision.Message,
		})
	}
}

func (h *Handler) handleReport(w http.ResponseWriter, r *http.Request) {
	var req ReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Report is fire-and-forget for audit logging.
	// The audit middleware on the route will capture the request.
	// We just return 200 OK.
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"received_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
