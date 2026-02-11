package conclave

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadConfig_Valid(t *testing.T) {
	content := `
server: wss://wardgate.example.com/conclaves/ws
key: secret-key
name: obsidian
max_input_bytes: 2097152
max_output_bytes: 5242880
allowed_bins:
  - cat
  - rg
`
	path := writeTempConfig(t, content)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server != "wss://wardgate.example.com/conclaves/ws" {
		t.Errorf("server: expected 'wss://wardgate.example.com/conclaves/ws', got %q", cfg.Server)
	}
	if cfg.Key != "secret-key" {
		t.Errorf("key: expected 'secret-key', got %q", cfg.Key)
	}
	if cfg.Name != "obsidian" {
		t.Errorf("name: expected 'obsidian', got %q", cfg.Name)
	}
	if cfg.MaxInputBytes != 2097152 {
		t.Errorf("max_input_bytes: expected 2097152, got %d", cfg.MaxInputBytes)
	}
	if cfg.MaxOutputBytes != 5242880 {
		t.Errorf("max_output_bytes: expected 5242880, got %d", cfg.MaxOutputBytes)
	}
	if len(cfg.AllowedBins) != 2 {
		t.Errorf("allowed_bins: expected 2 entries, got %d", len(cfg.AllowedBins))
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	content := `
server: wss://wardgate.example.com/conclaves/ws
key: secret-key
name: test
`
	path := writeTempConfig(t, content)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.MaxInputBytes != DefaultMaxInputBytes {
		t.Errorf("max_input_bytes: expected default %d, got %d", DefaultMaxInputBytes, cfg.MaxInputBytes)
	}
	if cfg.MaxOutputBytes != DefaultMaxOutputBytes {
		t.Errorf("max_output_bytes: expected default %d, got %d", DefaultMaxOutputBytes, cfg.MaxOutputBytes)
	}
}

func TestLoadConfig_MissingServer(t *testing.T) {
	content := `
key: secret-key
name: test
`
	path := writeTempConfig(t, content)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing server")
	}
	if !strings.Contains(err.Error(), "server") {
		t.Errorf("expected error about server, got: %v", err)
	}
}

func TestLoadConfig_MissingKey(t *testing.T) {
	content := `
server: wss://wardgate.example.com/conclaves/ws
name: test
`
	path := writeTempConfig(t, content)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "key") {
		t.Errorf("expected error about key, got: %v", err)
	}
}

func TestLoadConfig_MissingName(t *testing.T) {
	content := `
server: wss://wardgate.example.com/conclaves/ws
key: secret-key
`
	path := writeTempConfig(t, content)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
	if !strings.Contains(err.Error(), "name") {
		t.Errorf("expected error about name, got: %v", err)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}
	return path
}
