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

// Preset tests for Phase 6 - Plugin architecture for non-tech savvy users
// Presets are now loaded from YAML files in the presets/ directory

func TestLoadConfig_PresetTodoist(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  my-todoist:
    preset: todoist
    auth:
      credential_env: MY_TODOIST_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep, ok := cfg.Endpoints["my-todoist"]
	if !ok {
		t.Fatal("expected endpoint 'my-todoist'")
	}

	// Should have preset defaults applied
	if ep.Upstream != "https://api.todoist.com/api/v1" {
		t.Errorf("expected Todoist upstream, got %s", ep.Upstream)
	}
	if ep.Auth.Type != "bearer" {
		t.Errorf("expected auth type 'bearer', got %s", ep.Auth.Type)
	}
	if ep.Auth.CredentialEnv != "MY_TODOIST_KEY" {
		t.Errorf("expected credential_env 'MY_TODOIST_KEY', got %s", ep.Auth.CredentialEnv)
	}
	// Should have default rules from preset
	if len(ep.Rules) == 0 {
		t.Error("expected default rules from preset")
	}
}

func TestLoadConfig_PresetGitHub(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  github:
    preset: github
    auth:
      credential_env: GITHUB_TOKEN
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["github"]
	if ep.Upstream != "https://api.github.com" {
		t.Errorf("expected GitHub upstream, got %s", ep.Upstream)
	}
	if ep.Auth.Type != "bearer" {
		t.Errorf("expected auth type 'bearer', got %s", ep.Auth.Type)
	}
}

func TestLoadConfig_PresetCloudflare(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  cloudflare:
    preset: cloudflare
    auth:
      credential_env: CF_TOKEN
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["cloudflare"]
	if ep.Upstream != "https://api.cloudflare.com/client/v4" {
		t.Errorf("expected Cloudflare upstream, got %s", ep.Upstream)
	}
}

func TestLoadConfig_PresetOverrideUpstream(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  custom-todoist:
    preset: todoist
    upstream: https://custom.todoist.proxy.com
    auth:
      credential_env: MY_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["custom-todoist"]
	// User override should take precedence
	if ep.Upstream != "https://custom.todoist.proxy.com" {
		t.Errorf("expected custom upstream, got %s", ep.Upstream)
	}
	// Auth type should still come from preset
	if ep.Auth.Type != "bearer" {
		t.Errorf("expected auth type 'bearer' from preset, got %s", ep.Auth.Type)
	}
}

func TestLoadConfig_PresetOverrideRules(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  strict-todoist:
    preset: todoist
    auth:
      credential_env: MY_KEY
    rules:
      - match: { method: GET }
        action: allow
      - match: { method: "*" }
        action: deny
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["strict-todoist"]
	// User rules should override preset rules
	if len(ep.Rules) != 2 {
		t.Errorf("expected 2 custom rules, got %d", len(ep.Rules))
	}
}

func TestLoadConfig_PresetUnknown(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  test:
    preset: unknown-service
    auth:
      credential_env: SOME_KEY
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for unknown preset")
	}
	if !strings.Contains(err.Error(), "unknown preset") {
		t.Errorf("error should mention 'unknown preset': %v", err)
	}
}

func TestLoadConfig_PresetMissingCredentialEnv(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  test:
    preset: todoist
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for missing credential_env")
	}
	if !strings.Contains(err.Error(), "auth") {
		t.Errorf("error should mention missing auth: %v", err)
	}
}

func TestLoadPresetsFromDir(t *testing.T) {
	presets, err := LoadPresetsFromDir("../../presets")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(presets) == 0 {
		t.Fatal("expected at least one preset")
	}

	// Check that todoist preset exists
	found := false
	for _, p := range presets {
		if p.Name == "todoist" {
			found = true
			if p.Description == "" {
				t.Error("preset should have a description")
			}
			if p.Upstream == "" {
				t.Error("preset should have upstream URL")
			}
			if p.AuthType == "" {
				t.Error("preset should have auth type")
			}
			break
		}
	}
	if !found {
		t.Error("expected 'todoist' preset to be available")
	}
}

func TestLoadConfig_PresetIMAP(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  mail:
    preset: imap
    upstream: imaps://imap.gmail.com:993
    auth:
      credential_env: IMAP_CREDS
    capabilities:
      list_folders: allow
      read_inbox: allow
      mark_read: ask
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["mail"]
	if ep.Adapter != "imap" {
		t.Errorf("expected adapter 'imap', got %s", ep.Adapter)
	}
	if ep.Upstream != "imaps://imap.gmail.com:993" {
		t.Errorf("expected custom upstream, got %s", ep.Upstream)
	}
}

func TestLoadConfig_PresetSMTP(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  mail-send:
    preset: smtp
    upstream: smtps://smtp.gmail.com:465
    auth:
      credential_env: SMTP_CREDS
    capabilities:
      send_email: ask
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["mail-send"]
	if ep.Adapter != "smtp" {
		t.Errorf("expected adapter 'smtp', got %s", ep.Adapter)
	}
}

// Capability tests - user-friendly rule configuration

func TestLoadConfig_PresetCapabilities(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  github:
    preset: github
    auth:
      credential_env: GITHUB_TOKEN
    capabilities:
      read_data: allow
      create_issues: allow
      create_comments: deny
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["github"]
	// Capabilities should be expanded into rules
	if len(ep.Rules) == 0 {
		t.Fatal("expected rules to be generated from capabilities")
	}

	// Check that rules were generated correctly
	foundGetAllow := false
	foundIssueAllow := false
	foundCommentDeny := false
	for _, rule := range ep.Rules {
		if rule.Match.Method == "GET" && rule.Action == "allow" {
			foundGetAllow = true
		}
		if rule.Match.Path == "/repos/*/issues" && rule.Match.Method == "POST" && rule.Action == "allow" {
			foundIssueAllow = true
		}
		if rule.Match.Path == "/repos/*/issues/*/comments" && rule.Match.Method == "POST" && rule.Action == "deny" {
			foundCommentDeny = true
		}
	}
	if !foundGetAllow {
		t.Error("expected read_data capability to generate GET allow rule")
	}
	if !foundIssueAllow {
		t.Error("expected create_issues capability to generate issue POST allow rule")
	}
	if !foundCommentDeny {
		t.Error("expected create_comments: deny to generate comment POST deny rule")
	}
}

func TestLoadConfig_PresetCapabilitiesAsk(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  todoist:
    preset: todoist
    auth:
      credential_env: TODOIST_KEY
    capabilities:
      read_data: allow
      create_tasks: ask
      close_tasks: deny
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["todoist"]
	foundCreateAsk := false
	foundCloseDeny := false
	for _, rule := range ep.Rules {
		if rule.Match.Path == "/tasks" && rule.Match.Method == "POST" && rule.Action == "ask" {
			foundCreateAsk = true
		}
		if rule.Match.Path == "/tasks/*/close" && rule.Match.Method == "POST" && rule.Action == "deny" {
			foundCloseDeny = true
		}
	}
	if !foundCreateAsk {
		t.Error("expected create_tasks: ask capability")
	}
	if !foundCloseDeny {
		t.Error("expected close_tasks: deny capability")
	}
}

func TestLoadConfig_PresetUnknownCapability(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  github:
    preset: github
    auth:
      credential_env: GITHUB_TOKEN
    capabilities:
      nonexistent_capability: allow
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for unknown capability")
	}
	if !strings.Contains(err.Error(), "unknown capability") {
		t.Errorf("error should mention 'unknown capability': %v", err)
	}
}

func TestLoadConfig_PresetCapabilitiesOverrideDefaultRules(t *testing.T) {
	// When capabilities are specified, they should replace default rules
	yaml := `
presets_dir: ../../presets
endpoints:
  github:
    preset: github
    auth:
      credential_env: GITHUB_TOKEN
    capabilities:
      read_data: deny
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["github"]
	// Should have rules from capabilities, not default preset rules
	foundGetDeny := false
	for _, rule := range ep.Rules {
		if rule.Match.Method == "GET" && rule.Action == "deny" {
			foundGetDeny = true
		}
	}
	if !foundGetDeny {
		t.Error("expected read_data: deny to override default allow")
	}
}

// Custom preset tests - user-defined presets

func TestLoadConfig_InlineCustomPreset(t *testing.T) {
	yaml := `
custom_presets:
  helpscout:
    description: "Help Scout API"
    upstream: https://api.helpscout.net/v2
    auth_type: bearer
    capabilities:
      - name: read_conversations
        description: "Read conversations"
        rules:
          - match: { method: GET, path: "/conversations*" }
      - name: reply
        description: "Reply to conversations"
        rules:
          - match: { method: POST, path: "/conversations/*/reply" }
    default_rules:
      - match: { method: GET }
        action: allow
      - match: { method: "*" }
        action: deny

endpoints:
  helpscout:
    preset: helpscout
    auth:
      credential_env: HELPSCOUT_TOKEN
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["helpscout"]
	if ep.Upstream != "https://api.helpscout.net/v2" {
		t.Errorf("expected custom preset upstream, got %s", ep.Upstream)
	}
	if ep.Auth.Type != "bearer" {
		t.Errorf("expected auth type bearer, got %s", ep.Auth.Type)
	}
}

func TestLoadConfig_InlineCustomPresetWithCapabilities(t *testing.T) {
	yaml := `
custom_presets:
  myapi:
    description: "My Custom API"
    upstream: https://api.example.com
    auth_type: bearer
    capabilities:
      - name: read_data
        description: "Read data"
        rules:
          - match: { method: GET }
      - name: write_data
        description: "Write data"
        rules:
          - match: { method: POST }

endpoints:
  myapi:
    preset: myapi
    auth:
      credential_env: MY_API_KEY
    capabilities:
      read_data: allow
      write_data: deny
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["myapi"]
	// Should have rules from capabilities
	foundGetAllow := false
	foundPostDeny := false
	for _, rule := range ep.Rules {
		if rule.Match.Method == "GET" && rule.Action == "allow" {
			foundGetAllow = true
		}
		if rule.Match.Method == "POST" && rule.Action == "deny" {
			foundPostDeny = true
		}
	}
	if !foundGetAllow {
		t.Error("expected read_data: allow to generate GET allow rule")
	}
	if !foundPostDeny {
		t.Error("expected write_data: deny to generate POST deny rule")
	}
}

func TestLoadConfig_CustomPresetOverridesBuiltin(t *testing.T) {
	// Custom preset with same name as builtin should override
	yaml := `
custom_presets:
  todoist:
    description: "Custom Todoist override"
    upstream: https://custom.todoist.example.com
    auth_type: bearer

endpoints:
  todoist:
    preset: todoist
    auth:
      credential_env: TODOIST_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["todoist"]
	// Should use custom preset upstream, not builtin
	if ep.Upstream != "https://custom.todoist.example.com" {
		t.Errorf("expected custom preset to override builtin, got upstream %s", ep.Upstream)
	}
}

func TestLoadConfig_ExternalPresetFile(t *testing.T) {
	// Create temp directory with preset file
	tmpDir, err := os.MkdirTemp("", "presets")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create preset file
	presetContent := `
name: linear
description: "Linear issue tracking API"
upstream: https://api.linear.app
auth_type: bearer
capabilities:
  - name: read_issues
    description: "Read issues"
    rules:
      - match: { method: GET, path: "/issues*" }
  - name: create_issues
    description: "Create issues"
    rules:
      - match: { method: POST, path: "/issues" }
default_rules:
  - match: { method: GET }
    action: allow
  - match: { method: "*" }
    action: deny
`
	if err := os.WriteFile(tmpDir+"/linear.yaml", []byte(presetContent), 0644); err != nil {
		t.Fatal(err)
	}

	yaml := `
presets_dir: ` + tmpDir + `

endpoints:
  linear:
    preset: linear
    auth:
      credential_env: LINEAR_TOKEN
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["linear"]
	if ep.Upstream != "https://api.linear.app" {
		t.Errorf("expected external preset upstream, got %s", ep.Upstream)
	}
}

func TestLoadConfig_ExternalPresetWithCapabilities(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "presets")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	presetContent := `
name: myservice
description: "My Service API"
upstream: https://api.myservice.com
auth_type: bearer
capabilities:
  - name: read_data
    description: "Read data"
    rules:
      - match: { method: GET }
  - name: delete_data
    description: "Delete data"
    rules:
      - match: { method: DELETE }
`
	if err := os.WriteFile(tmpDir+"/myservice.yaml", []byte(presetContent), 0644); err != nil {
		t.Fatal(err)
	}

	yaml := `
presets_dir: ` + tmpDir + `

endpoints:
  myservice:
    preset: myservice
    auth:
      credential_env: MYSERVICE_KEY
    capabilities:
      read_data: allow
      delete_data: ask
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["myservice"]
	foundGetAllow := false
	foundDeleteAsk := false
	for _, rule := range ep.Rules {
		if rule.Match.Method == "GET" && rule.Action == "allow" {
			foundGetAllow = true
		}
		if rule.Match.Method == "DELETE" && rule.Action == "ask" {
			foundDeleteAsk = true
		}
	}
	if !foundGetAllow {
		t.Error("expected read_data: allow")
	}
	if !foundDeleteAsk {
		t.Error("expected delete_data: ask")
	}
}

func TestLoadConfig_PresetPriority(t *testing.T) {
	// Priority: inline custom > external file > builtin
	tmpDir, err := os.MkdirTemp("", "presets")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// External preset file for todoist
	presetContent := `
name: todoist
description: "External Todoist"
upstream: https://external.todoist.example.com
auth_type: bearer
`
	if err := os.WriteFile(tmpDir+"/todoist.yaml", []byte(presetContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Inline custom preset should win
	yaml := `
presets_dir: ` + tmpDir + `

custom_presets:
  todoist:
    description: "Inline Todoist"
    upstream: https://inline.todoist.example.com
    auth_type: bearer

endpoints:
  todoist:
    preset: todoist
    auth:
      credential_env: TODOIST_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["todoist"]
	// Inline should win over external and builtin
	if ep.Upstream != "https://inline.todoist.example.com" {
		t.Errorf("expected inline preset to have highest priority, got %s", ep.Upstream)
	}
}

func TestLoadConfig_InvalidPresetsDir(t *testing.T) {
	yaml := `
presets_dir: /nonexistent/path/that/does/not/exist

endpoints:
  test:
    upstream: https://example.com
    auth:
      type: bearer
      credential_env: TEST_KEY
`
	// Should not error - just log warning and continue
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("should not error on missing presets_dir: %v", err)
	}
}

func TestLoadConfig_CustomPresetUnknownCapability(t *testing.T) {
	yaml := `
custom_presets:
  myapi:
    description: "My API"
    upstream: https://api.example.com
    auth_type: bearer
    capabilities:
      - name: read_data
        description: "Read"
        rules:
          - match: { method: GET }

endpoints:
  myapi:
    preset: myapi
    auth:
      credential_env: MY_KEY
    capabilities:
      nonexistent: allow
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for unknown capability")
	}
	if !strings.Contains(err.Error(), "unknown capability") {
		t.Errorf("error should mention unknown capability: %v", err)
	}
}

// GetEndpointDescription tests - Phase 8

// Agent scoping tests

func TestLoadConfig_EndpointAgents(t *testing.T) {
	yaml := `
endpoints:
  todoist:
    agents: [tessa, bob]
    upstream: https://api.todoist.com/rest/v2
    auth:
      type: bearer
      credential_env: TODOIST_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["todoist"]
	if len(ep.Agents) != 2 {
		t.Fatalf("expected 2 agents, got %d", len(ep.Agents))
	}
	if ep.Agents[0] != "tessa" || ep.Agents[1] != "bob" {
		t.Errorf("expected agents [tessa, bob], got %v", ep.Agents)
	}
}

func TestLoadConfig_ConclaveAgents(t *testing.T) {
	os.Setenv("TEST_CC_KEY", "test")
	t.Cleanup(func() { os.Unsetenv("TEST_CC_KEY") })

	yaml := `
conclaves:
  obsidian:
    agents: [tessa]
    key_env: TEST_CC_KEY
    rules:
      - match: { command: "*" }
        action: deny
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cc := cfg.Conclaves["obsidian"]
	if len(cc.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(cc.Agents))
	}
	if cc.Agents[0] != "tessa" {
		t.Errorf("expected agent 'tessa', got %s", cc.Agents[0])
	}
}

func TestLoadConfig_AgentsOmitted(t *testing.T) {
	yaml := `
endpoints:
  todoist:
    upstream: https://api.todoist.com/rest/v2
    auth:
      type: bearer
      credential_env: TODOIST_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["todoist"]
	if ep.Agents != nil && len(ep.Agents) != 0 {
		t.Errorf("expected nil/empty agents when omitted, got %v", ep.Agents)
	}
}

func TestGetEndpointDescription_ExplicitDescription(t *testing.T) {
	yaml := `
endpoints:
  my-api:
    description: "My Custom API"
    upstream: https://api.example.com
    auth:
      type: bearer
      credential_env: MY_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["my-api"]
	desc := cfg.GetEndpointDescription("my-api", ep)
	if desc != "My Custom API" {
		t.Errorf("expected 'My Custom API', got %s", desc)
	}
}

func TestGetEndpointDescription_PresetFallback(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  todoist:
    preset: todoist
    auth:
      credential_env: TODOIST_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["todoist"]
	desc := cfg.GetEndpointDescription("todoist", ep)
	// Should use preset description
	if desc == "" || desc == "HTTP" {
		t.Errorf("expected preset description, got %s", desc)
	}
}

func TestGetEndpointDescription_AdapterFallback(t *testing.T) {
	yaml := `
endpoints:
  mail:
    adapter: imap
    upstream: imaps://imap.example.com:993
    auth:
      type: plain
      credential_env: IMAP_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["mail"]
	desc := cfg.GetEndpointDescription("mail", ep)
	if desc != "IMAP" {
		t.Errorf("expected 'IMAP', got %s", desc)
	}
}

func TestGetEndpointDescription_HTTPDefault(t *testing.T) {
	yaml := `
endpoints:
  custom:
    upstream: https://api.example.com
    auth:
      type: bearer
      credential_env: API_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["custom"]
	desc := cfg.GetEndpointDescription("custom", ep)
	if desc != "HTTP" {
		t.Errorf("expected 'HTTP', got %s", desc)
	}
}

func TestGetEndpointDescription_ExplicitOverridesPreset(t *testing.T) {
	yaml := `
presets_dir: ../../presets
endpoints:
  todoist:
    preset: todoist
    description: "Paul's Tasks"
    auth:
      credential_env: TODOIST_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["todoist"]
	desc := cfg.GetEndpointDescription("todoist", ep)
	if desc != "Paul's Tasks" {
		t.Errorf("expected 'Paul's Tasks', got %s", desc)
	}
}
