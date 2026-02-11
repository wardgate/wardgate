package manage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateKey(t *testing.T) {
	key1, err := GenerateKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(key1) != 64 { // 32 bytes hex-encoded
		t.Errorf("expected 64 char hex key, got %d chars", len(key1))
	}

	key2, err := GenerateKey()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key1 == key2 {
		t.Error("expected unique keys across calls")
	}
}

func TestAppendEnvVar(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	os.WriteFile(envPath, []byte("EXISTING_VAR=hello\n"), 0644)

	if err := AppendEnvVar(envPath, "NEW_VAR", "world"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(envPath)
	content := string(data)
	if !strings.Contains(content, "EXISTING_VAR=hello") {
		t.Error("existing content should be preserved")
	}
	if !strings.Contains(content, "NEW_VAR=world") {
		t.Error("new var should be appended")
	}
}

func TestAppendEnvVar_NoDuplicates(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	os.WriteFile(envPath, []byte("MY_VAR=existing\n"), 0644)

	err := AppendEnvVar(envPath, "MY_VAR", "new-value")
	if err == nil {
		t.Fatal("expected error for duplicate env var")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error should mention 'already exists': %v", err)
	}
}

func TestAppendEnvVar_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")

	if err := AppendEnvVar(envPath, "NEW_VAR", "value"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(envPath)
	if !strings.Contains(string(data), "NEW_VAR=value") {
		t.Error("should create file with var")
	}
}

func TestRemoveEnvVar(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	os.WriteFile(envPath, []byte("KEEP=yes\nREMOVE_ME=gone\nALSO_KEEP=yep\n"), 0644)

	if err := RemoveEnvVar(envPath, "REMOVE_ME"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(envPath)
	content := string(data)
	if strings.Contains(content, "REMOVE_ME") {
		t.Error("removed var should be gone")
	}
	if !strings.Contains(content, "KEEP=yes") {
		t.Error("other vars should be preserved")
	}
	if !strings.Contains(content, "ALSO_KEEP=yep") {
		t.Error("other vars should be preserved")
	}
}

func TestAddAgentToConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`server:
  listen: ":8080"
agents:
  - id: existing
    key_env: EXISTING_KEY
endpoints: {}
`), 0644)

	err := AddAgent(cfgPath, "new-agent", "NEW_AGENT_KEY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if !strings.Contains(content, "new-agent") {
		t.Error("new agent should appear in config")
	}
	if !strings.Contains(content, "NEW_AGENT_KEY") {
		t.Error("key_env should appear in config")
	}
	if !strings.Contains(content, "existing") {
		t.Error("existing agent should be preserved")
	}
}

func TestAddConclaveToConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`server:
  listen: ":8080"
agents: []
endpoints: {}
`), 0644)

	err := AddConclave(cfgPath, "obsidian", "OBSIDIAN_KEY", "Personal notes")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if !strings.Contains(content, "obsidian") {
		t.Error("conclave should appear in config")
	}
	if !strings.Contains(content, "OBSIDIAN_KEY") {
		t.Error("key_env should appear in config")
	}
	if !strings.Contains(content, "Personal notes") {
		t.Error("description should appear in config")
	}
}

func TestRemoveAgentFromConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`server:
  listen: ":8080"
agents:
  - id: keep-me
    key_env: KEEP_KEY
  - id: remove-me
    key_env: REMOVE_KEY
endpoints: {}
`), 0644)

	err := RemoveAgent(cfgPath, "remove-me")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if strings.Contains(content, "remove-me") {
		t.Error("removed agent should be gone")
	}
	if !strings.Contains(content, "keep-me") {
		t.Error("other agents should be preserved")
	}
}

func TestRemoveConclaveFromConfig(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`server:
  listen: ":8080"
agents: []
endpoints: {}
conclaves:
  keep:
    key_env: KEEP_KEY
    description: "Keep this"
  remove:
    key_env: REMOVE_KEY
    description: "Remove this"
`), 0644)

	err := RemoveConclave(cfgPath, "remove")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	data, _ := os.ReadFile(cfgPath)
	content := string(data)
	if strings.Contains(content, "Remove this") {
		t.Error("removed conclave should be gone")
	}
	if !strings.Contains(content, "Keep this") {
		t.Error("other conclaves should be preserved")
	}
}
