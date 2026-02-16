package auth

import (
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/wardgate/wardgate/internal/config"
)

// AgentAuthMiddleware validates agent authentication.
type AgentAuthMiddleware struct {
	validKeys map[string]string // key -> agent ID
	jwtSecret []byte            // HMAC secret for JWT validation (nil = disabled)
	jwtIssuer string            // expected issuer (empty = skip check)
	jwtAud    string            // expected audience (empty = skip check)
	next      http.Handler
}

// NewAgentAuthMiddleware creates a new agent auth middleware.
func NewAgentAuthMiddleware(agents []config.AgentConfig, jwtCfg *config.JWTConfig, next http.Handler) *AgentAuthMiddleware {
	validKeys := make(map[string]string)
	for _, agent := range agents {
		key := os.Getenv(agent.KeyEnv)
		if key != "" {
			validKeys[key] = agent.ID
		}
	}

	m := &AgentAuthMiddleware{
		validKeys: validKeys,
		next:      next,
	}

	if jwtCfg != nil {
		secret := jwtCfg.Secret
		if secret == "" && jwtCfg.SecretEnv != "" {
			secret = os.Getenv(jwtCfg.SecretEnv)
		}
		if secret != "" {
			m.jwtSecret = []byte(secret)
			m.jwtIssuer = jwtCfg.Issuer
			m.jwtAud = jwtCfg.Audience
		}
	}

	return m
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

	// Try static key lookup first (fast path)
	if agentID, ok := m.validKeys[token]; ok {
		r.Header.Set("X-Agent-ID", agentID)
		m.next.ServeHTTP(w, r)
		return
	}

	// Try JWT validation if configured
	if m.jwtSecret != nil {
		if agentID, err := m.validateJWT(token); err == nil {
			r.Header.Set("X-Agent-ID", agentID)
			m.next.ServeHTTP(w, r)
			return
		}
	}

	http.Error(w, "invalid agent key", http.StatusUnauthorized)
}

// validateJWT parses and validates a JWT token, returning the agent ID from the "sub" claim.
func (m *AgentAuthMiddleware) validateJWT(tokenStr string) (string, error) {
	opts := []jwt.ParserOption{
		jwt.WithValidMethods([]string{"HS256", "HS384", "HS512"}),
	}
	if m.jwtIssuer != "" {
		opts = append(opts, jwt.WithIssuer(m.jwtIssuer))
	}
	if m.jwtAud != "" {
		opts = append(opts, jwt.WithAudience(m.jwtAud))
	}

	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return m.jwtSecret, nil
	}, opts...)
	if err != nil {
		return "", fmt.Errorf("jwt validation failed: %w", err)
	}

	sub, err := token.Claims.GetSubject()
	if err != nil || sub == "" {
		return "", fmt.Errorf("jwt missing sub claim")
	}

	return sub, nil
}
