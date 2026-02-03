package config

import (
	"os"
	"strings"
	"testing"
)

func TestLoadConfig_ValidYAML(t *testing.T) {
	yaml := `
server:
  listen: ":8080"

agents:
  - id: tessa
    key_env: WARDGATE_AGENT_TESSA_KEY

endpoints:
  todoist-api:
    upstream: https://api.todoist.com/rest/v2
    auth:
      type: bearer
      credential_env: WARDGATE_CRED_TODOIST_API_KEY
    rules:
      - match:
          method: GET
        action: allow
      - match:
          method: POST
          path: "/tasks"
        action: allow
      - match:
          method: "*"
        action: deny
        message: "Not permitted"
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Listen != ":8080" {
		t.Errorf("expected listen :8080, got %s", cfg.Server.Listen)
	}

	if len(cfg.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(cfg.Agents))
	}
	if cfg.Agents[0].ID != "tessa" {
		t.Errorf("expected agent id 'tessa', got %s", cfg.Agents[0].ID)
	}

	ep, ok := cfg.Endpoints["todoist-api"]
	if !ok {
		t.Fatal("expected endpoint 'todoist-api'")
	}
	if ep.Upstream != "https://api.todoist.com/rest/v2" {
		t.Errorf("unexpected upstream: %s", ep.Upstream)
	}
	if ep.Auth.Type != "bearer" {
		t.Errorf("expected auth type 'bearer', got %s", ep.Auth.Type)
	}
	if len(ep.Rules) != 3 {
		t.Errorf("expected 3 rules, got %d", len(ep.Rules))
	}
}

func TestLoadConfig_MissingUpstream(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
endpoints:
  bad-endpoint:
    auth:
      type: bearer
      credential_env: SOME_KEY
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for missing upstream")
	}
	if !strings.Contains(err.Error(), "upstream") {
		t.Errorf("error should mention 'upstream': %v", err)
	}
}

func TestLoadConfig_MissingAuth(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
endpoints:
  bad-endpoint:
    upstream: https://example.com
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for missing auth")
	}
	if !strings.Contains(err.Error(), "auth") {
		t.Errorf("error should mention 'auth': %v", err)
	}
}

func TestLoadConfig_InvalidAction(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
endpoints:
  test-endpoint:
    upstream: https://example.com
    auth:
      type: bearer
      credential_env: SOME_KEY
    rules:
      - match:
          method: GET
        action: invalid_action
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for invalid action")
	}
	if !strings.Contains(err.Error(), "action") {
		t.Errorf("error should mention 'action': %v", err)
	}
}

func TestLoadConfig_DefaultListenPort(t *testing.T) {
	yaml := `
endpoints:
  test-endpoint:
    upstream: https://example.com
    auth:
      type: bearer
      credential_env: SOME_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Listen != ":8080" {
		t.Errorf("expected default listen :8080, got %s", cfg.Server.Listen)
	}
}

func TestLoadFromFile(t *testing.T) {
	content := `
server:
  listen: ":9090"
endpoints:
  test:
    upstream: https://example.com
    auth:
      type: bearer
      credential_env: TEST_KEY
`
	tmpfile, err := os.CreateTemp("", "config*.yaml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpfile.Close()

	cfg, err := LoadFromFile(tmpfile.Name())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Server.Listen)
	}
}

func TestLoadFromFile_NotFound(t *testing.T) {
	_, err := LoadFromFile("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}
