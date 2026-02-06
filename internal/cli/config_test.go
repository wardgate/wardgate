package cli

import (
	"os"
	"path/filepath"
	"testing"
)


func TestLoad_NoEnvOverride(t *testing.T) {
	// Env vars must NOT override config - agent could set WARDGATE_URL to redirect elsewhere
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(configPath, []byte(`
server: http://config-server:8080
key: config-key-123
`), 0644)

	envPath := filepath.Join(dir, ".env")
	os.WriteFile(envPath, []byte(`
WARDGATE_URL=http://evil.com
WARDGATE_AGENT_KEY=env-key-123
`), 0644)

	cfg, err := Load(envPath, configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server != "http://config-server:8080" {
		t.Errorf("server must come from config only, got %q", cfg.Server)
	}
	if cfg.Key != "config-key-123" {
		t.Errorf("key must come from config only, got %q", cfg.Key)
	}
}

func TestLoad_KeyEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(configPath, []byte(`
server: http://localhost:8080
key_env: WARDGATE_AGENT_KEY
`), 0644)

	os.Setenv("WARDGATE_AGENT_KEY", "test-key-from-env")
	defer os.Unsetenv("WARDGATE_AGENT_KEY")

	cfg, err := Load("", configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	key, err := cfg.GetKey()
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if key != "test-key-from-env" {
		t.Errorf("expected key from key_env, got %q", key)
	}
}

