package discovery

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/wardgate/wardgate/internal/auth"
)

// EndpointInfo describes an available endpoint for agents.
type EndpointInfo struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Upstream    string   `json:"upstream,omitempty"`    // Base URL of the upstream API (for version info)
	DocsURL     string   `json:"docs_url"`              // Link to API documentation (empty if none)
	Agents      []string `json:"-"`                     // Restrict visibility to specific agents (not serialized)
}

// EndpointsResponse is the response for GET /endpoints.
type EndpointsResponse struct {
	Endpoints []EndpointInfo `json:"endpoints"`
}

// Handler handles discovery API requests.
type Handler struct {
	endpoints []EndpointInfo
}

// NewHandler creates a new discovery handler.
func NewHandler(endpoints []EndpointInfo) *Handler {
	return &Handler{endpoints: endpoints}
}

// ServeHTTP handles incoming requests.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/")

	switch path {
	case "endpoints", "":
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.handleListEndpoints(w, r)
	default:
		http.Error(w, "not found", http.StatusNotFound)
	}
}

func (h *Handler) handleListEndpoints(w http.ResponseWriter, r *http.Request) {
	agentID := r.Header.Get("X-Agent-ID")

	var filtered []EndpointInfo
	for _, ep := range h.endpoints {
		if auth.AgentAllowed(ep.Agents, agentID) {
			filtered = append(filtered, ep)
		}
	}

	resp := EndpointsResponse{
		Endpoints: filtered,
	}
	if resp.Endpoints == nil {
		resp.Endpoints = []EndpointInfo{}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
