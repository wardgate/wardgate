package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/wardgate/wardgate/internal/config"
)

const testJWTSecret = "test-jwt-secret-32-bytes-long!!"

func signJWT(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(testJWTSecret))
	if err != nil {
		t.Fatalf("failed to sign JWT: %v", err)
	}
	return s
}

func TestJWT_ValidToken(t *testing.T) {
	jwtCfg := &config.JWTConfig{Secret: testJWTSecret}

	var receivedAgentID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAgentID = r.Header.Get("X-Agent-ID")
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAgentAuthMiddleware(nil, jwtCfg, next)

	token := signJWT(t, jwt.MapClaims{
		"sub": "agent-sandbox-42",
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if receivedAgentID != "agent-sandbox-42" {
		t.Errorf("expected agent ID 'agent-sandbox-42', got '%s'", receivedAgentID)
	}
}

func TestJWT_ExpiredToken(t *testing.T) {
	jwtCfg := &config.JWTConfig{Secret: testJWTSecret}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for expired JWT")
	})

	middleware := NewAgentAuthMiddleware(nil, jwtCfg, next)

	token := signJWT(t, jwt.MapClaims{
		"sub": "agent-expired",
		"exp": time.Now().Add(-time.Hour).Unix(),
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestJWT_WrongSecret(t *testing.T) {
	jwtCfg := &config.JWTConfig{Secret: testJWTSecret}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for bad signature")
	})

	middleware := NewAgentAuthMiddleware(nil, jwtCfg, next)

	// Sign with a different secret
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": "agent-bad",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	s, _ := token.SignedString([]byte("wrong-secret-wrong-secret-wrong!"))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+s)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestJWT_MissingSub(t *testing.T) {
	jwtCfg := &config.JWTConfig{Secret: testJWTSecret}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called for missing sub")
	})

	middleware := NewAgentAuthMiddleware(nil, jwtCfg, next)

	token := signJWT(t, jwt.MapClaims{
		"exp": time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}

func TestJWT_IssuerValidation(t *testing.T) {
	jwtCfg := &config.JWTConfig{Secret: testJWTSecret, Issuer: "my-orchestrator"}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAgentAuthMiddleware(nil, jwtCfg, next)

	// Valid issuer
	token := signJWT(t, jwt.MapClaims{
		"sub": "agent-1",
		"iss": "my-orchestrator",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("valid issuer: expected 200, got %d", rec.Code)
	}

	// Wrong issuer
	token = signJWT(t, jwt.MapClaims{
		"sub": "agent-1",
		"iss": "wrong-issuer",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong issuer: expected 401, got %d", rec.Code)
	}
}

func TestJWT_AudienceValidation(t *testing.T) {
	jwtCfg := &config.JWTConfig{Secret: testJWTSecret, Audience: "wardgate"}

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAgentAuthMiddleware(nil, jwtCfg, next)

	// Valid audience
	token := signJWT(t, jwt.MapClaims{
		"sub": "agent-1",
		"aud": "wardgate",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("valid audience: expected 200, got %d", rec.Code)
	}

	// Wrong audience
	token = signJWT(t, jwt.MapClaims{
		"sub": "agent-1",
		"aud": "other-service",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	req = httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong audience: expected 401, got %d", rec.Code)
	}
}

func TestJWT_StaticKeyTakesPriority(t *testing.T) {
	t.Setenv("TEST_STATIC_KEY", "static-key-value")

	agents := []config.AgentConfig{
		{ID: "static-agent", KeyEnv: "TEST_STATIC_KEY"},
	}
	jwtCfg := &config.JWTConfig{Secret: testJWTSecret}

	var receivedAgentID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAgentID = r.Header.Get("X-Agent-ID")
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAgentAuthMiddleware(agents, jwtCfg, next)

	// Use the static key
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer static-key-value")
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if receivedAgentID != "static-agent" {
		t.Errorf("expected 'static-agent', got '%s'", receivedAgentID)
	}
}

func TestJWT_FallbackFromStaticKey(t *testing.T) {
	t.Setenv("TEST_STATIC_KEY", "static-key-value")

	agents := []config.AgentConfig{
		{ID: "static-agent", KeyEnv: "TEST_STATIC_KEY"},
	}
	jwtCfg := &config.JWTConfig{Secret: testJWTSecret}

	var receivedAgentID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAgentID = r.Header.Get("X-Agent-ID")
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAgentAuthMiddleware(agents, jwtCfg, next)

	// Use a JWT (not the static key)
	token := signJWT(t, jwt.MapClaims{
		"sub": "jwt-agent",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if receivedAgentID != "jwt-agent" {
		t.Errorf("expected 'jwt-agent', got '%s'", receivedAgentID)
	}
}

func TestJWT_SecretEnv(t *testing.T) {
	t.Setenv("MY_JWT_SECRET", testJWTSecret)

	jwtCfg := &config.JWTConfig{SecretEnv: "MY_JWT_SECRET"}

	var receivedAgentID string
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAgentID = r.Header.Get("X-Agent-ID")
		w.WriteHeader(http.StatusOK)
	})

	middleware := NewAgentAuthMiddleware(nil, jwtCfg, next)

	token := signJWT(t, jwt.MapClaims{
		"sub": "env-agent",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if receivedAgentID != "env-agent" {
		t.Errorf("expected 'env-agent', got '%s'", receivedAgentID)
	}
}

func TestJWT_DisabledWhenNoConfig(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	})

	// No JWT config, no static keys
	middleware := NewAgentAuthMiddleware(nil, nil, next)

	token := signJWT(t, jwt.MapClaims{
		"sub": "agent-1",
		"exp": time.Now().Add(time.Hour).Unix(),
	})
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rec.Code)
	}
}
