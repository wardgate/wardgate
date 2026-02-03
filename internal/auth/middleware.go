package auth

import (
	"net/http"
	"os"
	"strings"

	"github.com/wardgate/wardgate/internal/config"
)

// AgentAuthMiddleware validates agent authentication.
type AgentAuthMiddleware struct {
	validKeys map[string]string // key -> agent ID
	next      http.Handler
}

// NewAgentAuthMiddleware creates a new agent auth middleware.
func NewAgentAuthMiddleware(agents []config.AgentConfig, next http.Handler) *AgentAuthMiddleware {
	validKeys := make(map[string]string)
	for _, agent := range agents {
		key := os.Getenv(agent.KeyEnv)
		if key != "" {
			validKeys[key] = agent.ID
		}
	}
	return &AgentAuthMiddleware{
		validKeys: validKeys,
		next:      next,
	}
}

// ServeHTTP implements http.Handler.
func (m *AgentAuthMiddleware) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	authHeader := r.Header.Get("Authorization")
	if authHeader == "" {
		http.Error(w, "missing authorization header", http.StatusUnauthorized)
		return
	}

	if !strings.HasPrefix(authHeader, "Bearer ") {
		http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
		return
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	agentID, ok := m.validKeys[token]
	if !ok {
		http.Error(w, "invalid agent key", http.StatusUnauthorized)
		return
	}

	// Set agent ID for downstream handlers (rate limiting, audit, approvals)
	r.Header.Set("X-Agent-ID", agentID)

	m.next.ServeHTTP(w, r)
}
