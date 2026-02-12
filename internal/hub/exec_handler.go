package hub

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/wardgate/wardgate/internal/approval"
	"github.com/wardgate/wardgate/internal/auth"
	"github.com/wardgate/wardgate/internal/config"
	execpkg "github.com/wardgate/wardgate/internal/exec"
	"github.com/wardgate/wardgate/internal/grants"
	"github.com/wardgate/wardgate/internal/policy"
)

// ConclaveExecRequest is the JSON body for POST /conclaves/{name}/exec.
type ConclaveExecRequest struct {
	Cwd     string `json:"cwd,omitempty"`
	Raw     string `json:"raw"` // Command string - parsed and validated server-side
	AgentID string `json:"agent_id"`
}

// ConclaveExecResponse is the JSON response for conclave exec requests.
type ConclaveExecResponse struct {
	Action   string `json:"action"` // "allow", "deny", "ask", "error"
	Message  string `json:"message,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"` // nil when not executed
}

// ExecHandler handles conclave exec requests with policy evaluation and routing.
type ExecHandler struct {
	hub         *Hub
	engines     map[string]*policy.Engine        // conclave name -> policy engine
	configs     map[string]config.ConclaveConfig // conclave name -> config
	approvalMgr *approval.Manager
	grantStore  *grants.Store
}

// SetGrantStore sets the grant store for dynamic policy overrides.
func (h *ExecHandler) SetGrantStore(s *grants.Store) {
	h.grantStore = s
}

// NewExecHandler creates a new conclave exec handler.
func NewExecHandler(hub *Hub, conclaves map[string]config.ConclaveConfig) *ExecHandler {
	engines := make(map[string]*policy.Engine, len(conclaves))
	for name, cc := range conclaves {
		engines[name] = policy.New(cc.Rules)
	}
	return &ExecHandler{
		hub:     hub,
		engines: engines,
		configs: conclaves,
	}
}

// SetApprovalManager sets the approval manager for "ask" decisions.
func (h *ExecHandler) SetApprovalManager(mgr *approval.Manager) {
	h.approvalMgr = mgr
}

// ServeHTTP routes conclave requests.
// Handles: GET / (list conclaves), POST /{name}/exec
func (h *ExecHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")

	// GET / - list conclaves
	if (path == "" || path == "/") && r.Method == http.MethodGet {
		h.handleList(w, r)
		return
	}

	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// POST /{name}/exec or POST /{name}/run
	parts := strings.SplitN(path, "/", 2)
	if len(parts) != 2 || (parts[1] != "exec" && parts[1] != "run") {
		http.NotFound(w, r)
		return
	}
	conclaveName := parts[0]

	if parts[1] == "run" {
		h.handleRun(w, r, conclaveName)
		return
	}

	// Validate conclave exists
	cc, ok := h.configs[conclaveName]
	if !ok {
		writeJSON(w, http.StatusNotFound, ConclaveExecResponse{
			Action:  "error",
			Message: fmt.Sprintf("conclave %q not found", conclaveName),
		})
		return
	}

	// Check agent scope
	agentIDHeader := r.Header.Get("X-Agent-ID")
	if !auth.AgentAllowed(cc.Agents, agentIDHeader) {
		writeJSON(w, http.StatusForbidden, ConclaveExecResponse{
			Action:  "deny",
			Message: fmt.Sprintf("agent %q is not allowed to access conclave %q", agentIDHeader, conclaveName),
		})
		return
	}

	// Parse request
	var req ConclaveExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ConclaveExecResponse{
			Action:  "deny",
			Message: "invalid request body",
		})
		return
	}

	if req.Raw == "" {
		writeJSON(w, http.StatusBadRequest, ConclaveExecResponse{
			Action:  "deny",
			Message: "raw command required",
		})
		return
	}

	// Parse the raw command server-side (single source of truth for security).
	// SkipResolve: commands are resolved on the conclave, not the gateway.
	parsed, err := execpkg.ParseShellCommand(req.Raw, &execpkg.ParseOptions{
		AllowRedirects: cc.AllowRedirects,
		SkipResolve:    true,
	})
	if err != nil {
		writeJSON(w, http.StatusForbidden, ConclaveExecResponse{
			Action:  "deny",
			Message: err.Error(),
		})
		return
	}
	segments := parsed.Segments

	agentID := req.AgentID
	if agentID == "" {
		agentID = r.Header.Get("X-Agent-ID")
	}

	// Use conclave's default cwd if not specified
	if req.Cwd == "" {
		req.Cwd = cc.Cwd
	}

	// Evaluate policy for each segment. All must be allowed or ask.
	// If any segment requires ask, the entire command goes through approval.
	engine := h.engines[conclaveName]
	needsApproval := false

	for _, seg := range segments {
		// Check grants before static policy
		if h.grantStore != nil {
			if g := h.grantStore.CheckExec(agentID, "conclave:"+conclaveName, seg.Command, seg.Args, req.Cwd); g != nil {
				continue // grant allows this segment
			}
		}

		decision := engine.EvaluateExec(seg.Command, seg.Args, req.Cwd, agentID)

		switch decision.Action {
		case policy.Allow:
			// ok
		case policy.Ask:
			needsApproval = true
		case policy.Deny:
			writeJSON(w, http.StatusForbidden, ConclaveExecResponse{
				Action:  "deny",
				Message: fmt.Sprintf("%s: %s", seg.Command, decision.Message),
			})
			return
		case policy.RateLimited:
			writeJSON(w, http.StatusTooManyRequests, ConclaveExecResponse{
				Action:  "rate_limited",
				Message: fmt.Sprintf("%s: %s", seg.Command, decision.Message),
			})
			return
		default:
			writeJSON(w, http.StatusForbidden, ConclaveExecResponse{
				Action:  "deny",
				Message: fmt.Sprintf("%s: %s", seg.Command, decision.Message),
			})
			return
		}
	}

	if needsApproval {
		if h.approvalMgr == nil {
			writeJSON(w, http.StatusForbidden, ConclaveExecResponse{
				Action:  "deny",
				Message: "approval required but no approval manager configured",
			})
			return
		}

		approved, err := h.approvalMgr.RequestApprovalWithContent(r.Context(), approval.ApprovalRequest{
			Endpoint:    "conclave:" + conclaveName,
			Method:      "EXEC",
			Path:        segments[0].Command,
			AgentID:     agentID,
			ContentType: "exec",
			Summary:     fmt.Sprintf("Agent %s wants to execute on %s: %s", agentID, conclaveName, req.Raw),
			Body:        req.Raw,
			Headers: map[string]string{
				"Command":  segments[0].Command,
				"Args":     segments[0].Args,
				"Cwd":      req.Cwd,
				"Conclave": conclaveName,
			},
		})
		if err != nil {
			writeJSON(w, http.StatusGatewayTimeout, ConclaveExecResponse{
				Action:  "deny",
				Message: fmt.Sprintf("approval timeout: %v", err),
			})
			return
		}
		if !approved {
			writeJSON(w, http.StatusForbidden, ConclaveExecResponse{
				Action:  "deny",
				Message: "execution denied by approver",
			})
			return
		}
	}

	// Check conclave is connected
	if !h.hub.IsConnected(conclaveName) {
		writeJSON(w, http.StatusServiceUnavailable, ConclaveExecResponse{
			Action:  "error",
			Message: fmt.Sprintf("conclave %q is not connected", conclaveName),
		})
		return
	}

	// Generate request ID
	reqID := fmt.Sprintf("req_%d", time.Now().UnixNano())

	// Build the command to send to the conclave.
	// When redirects are allowed, use req.Raw to preserve them.
	// Otherwise, reconstruct from parsed segments (redirections were rejected
	// during parsing, so this is safe and avoids any raw-vs-segments mismatch).
	var execCmd, execArgs string
	if cc.AllowRedirects {
		execCmd = req.Raw
		execArgs = ""
	} else if len(segments) == 1 {
		execCmd = segments[0].Command
		execArgs = segments[0].Args
	} else {
		var parts []string
		for _, seg := range segments {
			if seg.Args != "" {
				parts = append(parts, seg.Command+" "+seg.Args)
			} else {
				parts = append(parts, seg.Command)
			}
		}
		execCmd = strings.Join(parts, " | ")
		execArgs = ""
	}

	ch, err := h.hub.SendExec(conclaveName, reqID, execCmd, execArgs, req.Cwd)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, ConclaveExecResponse{
			Action:  "error",
			Message: err.Error(),
		})
		return
	}
	defer h.hub.CleanupExec(conclaveName, reqID)

	// Collect output
	var stdout, stderr strings.Builder
	var exitCode int
	timeout := time.After(5 * time.Minute)

	for {
		select {
		case raw, ok := <-ch:
			if !ok {
				// Channel closed - conclave disconnected
				writeJSON(w, http.StatusServiceUnavailable, ConclaveExecResponse{
					Action:  "error",
					Message: fmt.Sprintf("conclave %q disconnected during execution", conclaveName),
				})
				return
			}

			var msg struct {
				Type       string `json:"type"`
				Data       string `json:"data,omitempty"`
				Code       int    `json:"code,omitempty"`
				DurationMs int64  `json:"duration_ms,omitempty"`
				Message    string `json:"message,omitempty"`
			}
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}

			switch msg.Type {
			case MsgStdout:
				stdout.WriteString(msg.Data)
			case MsgStderr:
				stderr.WriteString(msg.Data)
			case MsgExit:
				exitCode = msg.Code
				writeJSON(w, http.StatusOK, ConclaveExecResponse{
					Action:   "allow",
					Stdout:   stdout.String(),
					Stderr:   stderr.String(),
					ExitCode: &exitCode,
				})
				return
			case MsgError:
				writeJSON(w, http.StatusInternalServerError, ConclaveExecResponse{
					Action:  "error",
					Message: msg.Message,
				})
				return
			}

		case <-timeout:
			h.hub.SendKill(conclaveName, reqID)
			writeJSON(w, http.StatusGatewayTimeout, ConclaveExecResponse{
				Action:  "error",
				Message: "execution timed out",
			})
			return

		case <-r.Context().Done():
			h.hub.SendKill(conclaveName, reqID)
			return
		}
	}
}

// ConclaveRunRequest is the JSON body for POST /conclaves/{name}/run.
type ConclaveRunRequest struct {
	Command string   `json:"command"`        // Command name from config
	Args    []string `json:"args,omitempty"` // Positional arg values
	Cwd     string   `json:"cwd,omitempty"`
	AgentID string   `json:"agent_id"`
}

// handleRun executes a pre-defined command template on a conclave.
func (h *ExecHandler) handleRun(w http.ResponseWriter, r *http.Request, conclaveName string) {
	// Validate conclave exists
	cc, ok := h.configs[conclaveName]
	if !ok {
		writeJSON(w, http.StatusNotFound, ConclaveExecResponse{
			Action:  "error",
			Message: fmt.Sprintf("conclave %q not found", conclaveName),
		})
		return
	}

	// Check agent scope
	agentID := r.Header.Get("X-Agent-ID")
	if !auth.AgentAllowed(cc.Agents, agentID) {
		writeJSON(w, http.StatusForbidden, ConclaveExecResponse{
			Action:  "deny",
			Message: fmt.Sprintf("agent %q is not allowed to access conclave %q", agentID, conclaveName),
		})
		return
	}

	// Parse request
	var req ConclaveRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ConclaveExecResponse{
			Action:  "deny",
			Message: "invalid request body",
		})
		return
	}

	if req.Command == "" {
		writeJSON(w, http.StatusBadRequest, ConclaveExecResponse{
			Action:  "deny",
			Message: "command name required",
		})
		return
	}

	// Look up command definition
	cmdDef, ok := cc.Commands[req.Command]
	if !ok {
		writeJSON(w, http.StatusBadRequest, ConclaveExecResponse{
			Action:  "deny",
			Message: fmt.Sprintf("unknown command %q", req.Command),
		})
		return
	}

	// Expand template with provided args
	expanded, err := execpkg.ExpandTemplate(cmdDef.Template, cmdDef.Args, req.Args)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, ConclaveExecResponse{
			Action:  "deny",
			Message: err.Error(),
		})
		return
	}

	if req.AgentID != "" {
		agentID = req.AgentID
	}

	// Use conclave's default cwd if not specified
	cwd := req.Cwd
	if cwd == "" {
		cwd = cc.Cwd
	}

	// Check action: ask requires approval
	action := cmdDef.Action
	if action == "" {
		action = "allow"
	}

	if action == "ask" {
		if h.approvalMgr == nil {
			writeJSON(w, http.StatusForbidden, ConclaveExecResponse{
				Action:  "deny",
				Message: "approval required but no approval manager configured",
			})
			return
		}

		approved, err := h.approvalMgr.RequestApprovalWithContent(r.Context(), approval.ApprovalRequest{
			Endpoint:    "conclave:" + conclaveName,
			Method:      "RUN",
			Path:        req.Command,
			AgentID:     agentID,
			ContentType: "exec",
			Summary:     fmt.Sprintf("Agent %s wants to run %s on %s: %s", agentID, req.Command, conclaveName, expanded),
			Body:        expanded,
			Headers: map[string]string{
				"Command":  req.Command,
				"Expanded": expanded,
				"Cwd":      cwd,
				"Conclave": conclaveName,
			},
		})
		if err != nil {
			writeJSON(w, http.StatusGatewayTimeout, ConclaveExecResponse{
				Action:  "deny",
				Message: fmt.Sprintf("approval timeout: %v", err),
			})
			return
		}
		if !approved {
			writeJSON(w, http.StatusForbidden, ConclaveExecResponse{
				Action:  "deny",
				Message: "execution denied by approver",
			})
			return
		}
	}

	// Check conclave is connected
	if !h.hub.IsConnected(conclaveName) {
		writeJSON(w, http.StatusServiceUnavailable, ConclaveExecResponse{
			Action:  "error",
			Message: fmt.Sprintf("conclave %q is not connected", conclaveName),
		})
		return
	}

	// Send expanded command to conclave
	reqID := fmt.Sprintf("req_%d", time.Now().UnixNano())
	ch, err := h.hub.SendExec(conclaveName, reqID, expanded, "", cwd)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, ConclaveExecResponse{
			Action:  "error",
			Message: err.Error(),
		})
		return
	}
	defer h.hub.CleanupExec(conclaveName, reqID)

	// Collect output (same as exec handler)
	var stdout, stderr strings.Builder
	var exitCode int
	timeout := time.After(5 * time.Minute)

	for {
		select {
		case raw, ok := <-ch:
			if !ok {
				writeJSON(w, http.StatusServiceUnavailable, ConclaveExecResponse{
					Action:  "error",
					Message: fmt.Sprintf("conclave %q disconnected during execution", conclaveName),
				})
				return
			}

			var msg struct {
				Type       string `json:"type"`
				Data       string `json:"data,omitempty"`
				Code       int    `json:"code,omitempty"`
				DurationMs int64  `json:"duration_ms,omitempty"`
				Message    string `json:"message,omitempty"`
			}
			if err := json.Unmarshal(raw, &msg); err != nil {
				continue
			}

			switch msg.Type {
			case MsgStdout:
				stdout.WriteString(msg.Data)
			case MsgStderr:
				stderr.WriteString(msg.Data)
			case MsgExit:
				exitCode = msg.Code
				writeJSON(w, http.StatusOK, ConclaveExecResponse{
					Action:   "allow",
					Stdout:   stdout.String(),
					Stderr:   stderr.String(),
					ExitCode: &exitCode,
				})
				return
			case MsgError:
				writeJSON(w, http.StatusInternalServerError, ConclaveExecResponse{
					Action:  "error",
					Message: msg.Message,
				})
				return
			}

		case <-timeout:
			h.hub.SendKill(conclaveName, reqID)
			writeJSON(w, http.StatusGatewayTimeout, ConclaveExecResponse{
				Action:  "error",
				Message: "execution timed out",
			})
			return

		case <-r.Context().Done():
			h.hub.SendKill(conclaveName, reqID)
			return
		}
	}
}

// CommandInfo describes a defined command for discovery.
type CommandInfo struct {
	Name        string              `json:"name"`
	Description string              `json:"description,omitempty"`
	Args        []config.CommandArg `json:"args,omitempty"`
}

// ConclaveListItem is a single conclave in the list response.
type ConclaveListItem struct {
	Name        string        `json:"name"`
	Description string        `json:"description,omitempty"`
	Status      string        `json:"status"` // "connected" or "disconnected"
	Commands    []CommandInfo `json:"commands,omitempty"`
}

// handleList returns the list of configured conclaves with status.
func (h *ExecHandler) handleList(w http.ResponseWriter, r *http.Request) {
	agentID := r.Header.Get("X-Agent-ID")
	items := make([]ConclaveListItem, 0, len(h.configs))
	for name, cc := range h.configs {
		if !auth.AgentAllowed(cc.Agents, agentID) {
			continue
		}
		status := "disconnected"
		if h.hub.IsConnected(name) {
			status = "connected"
		}
		var cmds []CommandInfo
		for cmdName, cmdDef := range cc.Commands {
			cmds = append(cmds, CommandInfo{
				Name:        cmdName,
				Description: cmdDef.Description,
				Args:        cmdDef.Args,
			})
		}
		items = append(items, ConclaveListItem{
			Name:        name,
			Description: cc.Description,
			Status:      status,
			Commands:    cmds,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"conclaves": items,
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
