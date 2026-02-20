package ssh

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/wardgate/wardgate/internal/approval"
	"github.com/wardgate/wardgate/internal/filter"
	"github.com/wardgate/wardgate/internal/policy"
)

// PoolGetter is the interface for getting connections from the pool.
type PoolGetter interface {
	Get(endpoint string, cfg ConnectionConfig) (Client, error)
	Put(endpoint string, client Client)
}

// HandlerConfig configures the SSH handler.
type HandlerConfig struct {
	EndpointName     string
	ConnectionConfig ConnectionConfig
}

// Handler handles REST requests for SSH operations.
type Handler struct {
	pool      PoolGetter
	engine    *policy.Engine
	config    HandlerConfig
	filter    *filter.Filter
	approvals ApprovalRequester
}

// NewHandler creates a new SSH REST handler.
func NewHandler(pool PoolGetter, engine *policy.Engine, cfg HandlerConfig) *Handler {
	return &Handler{
		pool:   pool,
		engine: engine,
		config: cfg,
	}
}

// SetApprovalManager sets the approval manager for ask workflows.
func (h *Handler) SetApprovalManager(m ApprovalRequester) {
	h.approvals = m
}

// SetFilter sets the sensitive data filter for response filtering.
func (h *Handler) SetFilter(f *filter.Filter) {
	h.filter = f
}

// ServeHTTP handles incoming REST requests and routes them to SSH operations.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")

	switch {
	case path == "exec" && r.Method == "POST":
		h.handleExec(w, r)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *Handler) handleExec(w http.ResponseWriter, r *http.Request) {
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		agentID = r.RemoteAddr
	}

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

	var req ExecRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.Command == "" {
		http.Error(w, "command is required", http.StatusBadRequest)
		return
	}

	if decision.Action == policy.Ask {
		if h.approvals == nil {
			http.Error(w, "ask action requires approval manager configuration", http.StatusServiceUnavailable)
			return
		}

		summary := fmt.Sprintf("SSH exec on %s: %s", h.config.EndpointName, req.Command)
		if req.Cwd != "" {
			summary += fmt.Sprintf(" (cwd: %s)", req.Cwd)
		}

		approved, err := h.approvals.RequestApprovalWithContent(r.Context(), approval.ApprovalRequest{
			Endpoint:    h.config.EndpointName,
			Method:      r.Method,
			Path:        "/exec",
			AgentID:     agentID,
			ContentType: "ssh-exec",
			Summary:     summary,
			Body:        req.Command,
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

	client, err := h.pool.Get(h.config.EndpointName, h.config.ConnectionConfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SSH connection error for %s: %v\n", h.config.EndpointName, err)
		http.Error(w, "failed to connect to SSH server", http.StatusBadGateway)
		return
	}
	defer h.pool.Put(h.config.EndpointName, client)

	stdout, stderr, exitCode, err := client.Exec(r.Context(), req.Command, req.Cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SSH exec error for %s: %v\n", h.config.EndpointName, err)
		http.Error(w, "command execution failed", http.StatusBadGateway)
		return
	}

	if h.filter != nil && h.filter.Enabled() {
		stdoutMatches := h.filter.Scan(stdout)
		if h.filter.ShouldBlock(stdoutMatches) {
			http.Error(w, fmt.Sprintf("output blocked: %s", filter.MatchDescription(stdoutMatches)), http.StatusForbidden)
			return
		}
		if len(stdoutMatches) > 0 {
			stdout = h.filter.Apply(stdout, stdoutMatches)
		}

		stderrMatches := h.filter.Scan(stderr)
		if len(stderrMatches) > 0 {
			stderr = h.filter.Apply(stderr, stderrMatches)
		}
	}

	h.writeJSON(w, ExecResponse{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: exitCode,
	})
}

func (h *Handler) writeJSON(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(v)
}
