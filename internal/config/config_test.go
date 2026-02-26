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
		if rule.Match.Path == "/repos/*/*/issues" && rule.Match.Method == "POST" && rule.Action == "allow" {
			foundIssueAllow = true
		}
		if rule.Match.Path == "/repos/*/*/issues/*/comments" && rule.Match.Method == "POST" && rule.Action == "deny" {
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

func TestLoadConfig_PresetCapabilitiesComposeWithRules(t *testing.T) {
	// User-defined rules should be prepended before capability-expanded rules.
	// First-match-wins means user rules act as overrides.
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
    rules:
      - match: { method: GET, path: "/repos/*/*/secret-stuff" }
        action: deny
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["github"]
	if len(ep.Rules) < 3 {
		t.Fatalf("expected at least 3 rules (1 user + capability rules + catch-all), got %d", len(ep.Rules))
	}

	// First rule should be the user-defined deny rule
	first := ep.Rules[0]
	if first.Match.Path != "/repos/*/*/secret-stuff" || first.Action != "deny" {
		t.Errorf("expected first rule to be the user-defined deny, got path=%q action=%q", first.Match.Path, first.Action)
	}

	// Last rule should be the catch-all deny from expandCapabilities
	last := ep.Rules[len(ep.Rules)-1]
	if last.Match.Method != "*" || last.Action != "deny" {
		t.Errorf("expected last rule to be catch-all deny, got method=%q action=%q", last.Match.Method, last.Action)
	}

	// Capability rules should be present in between
	foundGetAllow := false
	foundIssueAllow := false
	for _, rule := range ep.Rules[1:] {
		if rule.Match.Method == "GET" && rule.Action == "allow" {
			foundGetAllow = true
		}
		if rule.Match.Path == "/repos/*/*/issues" && rule.Match.Method == "POST" && rule.Action == "allow" {
			foundIssueAllow = true
		}
	}
	if !foundGetAllow {
		t.Error("expected read_data capability rules after user rules")
	}
	if !foundIssueAllow {
		t.Error("expected create_issues capability rules after user rules")
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

// Conclave command template tests

func TestLoadConfig_ConclaveCommands(t *testing.T) {
	os.Setenv("TEST_CC_KEY", "test")
	t.Cleanup(func() { os.Unsetenv("TEST_CC_KEY") })

	yaml := `
conclaves:
  obsidian:
    key_env: TEST_CC_KEY
    commands:
      search:
        description: "Search notes by filename"
        template: "find . -iname {query}"
        args:
          - name: query
            description: "Filename pattern"
      grep:
        description: "Search note contents"
        template: "rg {pattern} | grep -v SECRET1 | grep -v SECRET2"
        args:
          - name: pattern
            description: "Text pattern"
        action: ask
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	cc := cfg.Conclaves["obsidian"]
	if len(cc.Commands) != 2 {
		t.Fatalf("expected 2 commands, got %d", len(cc.Commands))
	}

	search := cc.Commands["search"]
	if search.Template != "find . -iname {query}" {
		t.Errorf("unexpected template: %s", search.Template)
	}
	if len(search.Args) != 1 || search.Args[0].Name != "query" {
		t.Errorf("unexpected args: %v", search.Args)
	}
	if search.Action != "" {
		t.Errorf("expected empty action (defaults to allow), got %s", search.Action)
	}

	grep := cc.Commands["grep"]
	if grep.Action != "ask" {
		t.Errorf("expected action 'ask', got %s", grep.Action)
	}
}

func TestLoadConfig_ConclaveCommandEmptyTemplate(t *testing.T) {
	os.Setenv("TEST_CC_KEY", "test")
	t.Cleanup(func() { os.Unsetenv("TEST_CC_KEY") })

	yaml := `
conclaves:
  test:
    key_env: TEST_CC_KEY
    commands:
      bad:
        description: "Missing template"
        template: ""
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for empty template")
	}
	if !strings.Contains(err.Error(), "template") {
		t.Errorf("error should mention 'template': %v", err)
	}
}

func TestLoadConfig_ConclaveCommandInvalidAction(t *testing.T) {
	os.Setenv("TEST_CC_KEY", "test")
	t.Cleanup(func() { os.Unsetenv("TEST_CC_KEY") })

	yaml := `
conclaves:
  test:
    key_env: TEST_CC_KEY
    commands:
      bad:
        template: "echo {x}"
        args:
          - name: x
        action: invalid
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for invalid command action")
	}
	if !strings.Contains(err.Error(), "action") {
		t.Errorf("error should mention 'action': %v", err)
	}
}

// ValidateEnv tests - verify key_env references resolve to actual env vars

func TestValidateEnv_AgentKeyEnvMissing(t *testing.T) {
	os.Unsetenv("WARDGATE_NONEXISTENT_KEY")
	cfg := &Config{
		Agents: []AgentConfig{{ID: "test-agent", KeyEnv: "WARDGATE_NONEXISTENT_KEY"}},
	}
	err := cfg.ValidateEnv()
	if err == nil {
		t.Fatal("expected error for missing agent key_env")
	}
	if !strings.Contains(err.Error(), "WARDGATE_NONEXISTENT_KEY") {
		t.Errorf("error should mention env var name: %v", err)
	}
	if !strings.Contains(err.Error(), "test-agent") {
		t.Errorf("error should mention agent id: %v", err)
	}
}

func TestValidateEnv_AgentKeyEnvPresent(t *testing.T) {
	t.Setenv("TEST_VALIDATE_AGENT_KEY", "secret")
	cfg := &Config{
		Agents: []AgentConfig{{ID: "test-agent", KeyEnv: "TEST_VALIDATE_AGENT_KEY"}},
	}
	if err := cfg.ValidateEnv(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEnv_AgentKeyEnvEmpty(t *testing.T) {
	t.Setenv("TEST_VALIDATE_AGENT_KEY", "")
	cfg := &Config{
		Agents: []AgentConfig{{ID: "test-agent", KeyEnv: "TEST_VALIDATE_AGENT_KEY"}},
	}
	if err := cfg.ValidateEnv(); err != nil {
		t.Fatalf("expected no error for empty env var (just warning): %v", err)
	}
}

func TestValidateEnv_AgentKeyEnvBlank(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{ID: "test-agent", KeyEnv: ""}},
	}
	if err := cfg.ValidateEnv(); err != nil {
		t.Fatalf("expected no error when key_env field is blank: %v", err)
	}
}

func TestValidateEnv_ConclaveKeyEnvMissing(t *testing.T) {
	os.Unsetenv("WARDGATE_NONEXISTENT_CC_KEY")
	cfg := &Config{
		Conclaves: map[string]ConclaveConfig{
			"my-cc": {KeyEnv: "WARDGATE_NONEXISTENT_CC_KEY"},
		},
	}
	err := cfg.ValidateEnv()
	if err == nil {
		t.Fatal("expected error for missing conclave key_env")
	}
	if !strings.Contains(err.Error(), "WARDGATE_NONEXISTENT_CC_KEY") {
		t.Errorf("error should mention env var name: %v", err)
	}
}

func TestValidateEnv_ConclaveKeyEnvPresent(t *testing.T) {
	t.Setenv("TEST_VALIDATE_CC_KEY", "secret")
	cfg := &Config{
		Conclaves: map[string]ConclaveConfig{
			"my-cc": {KeyEnv: "TEST_VALIDATE_CC_KEY"},
		},
	}
	if err := cfg.ValidateEnv(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEnv_ConclaveKeyEnvEmpty(t *testing.T) {
	t.Setenv("TEST_VALIDATE_CC_KEY", "")
	cfg := &Config{
		Conclaves: map[string]ConclaveConfig{
			"my-cc": {KeyEnv: "TEST_VALIDATE_CC_KEY"},
		},
	}
	if err := cfg.ValidateEnv(); err != nil {
		t.Fatalf("expected no error for empty env var (just warning): %v", err)
	}
}

func TestValidateEnv_AdminKeyEnvMissing(t *testing.T) {
	os.Unsetenv("WARDGATE_NONEXISTENT_ADMIN_KEY")
	cfg := &Config{
		Server: ServerConfig{AdminKeyEnv: "WARDGATE_NONEXISTENT_ADMIN_KEY"},
	}
	err := cfg.ValidateEnv()
	if err == nil {
		t.Fatal("expected error for missing admin_key_env")
	}
	if !strings.Contains(err.Error(), "WARDGATE_NONEXISTENT_ADMIN_KEY") {
		t.Errorf("error should mention env var name: %v", err)
	}
}

func TestValidateEnv_AdminKeyEnvNotConfigured(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{AdminKeyEnv: ""},
	}
	if err := cfg.ValidateEnv(); err != nil {
		t.Fatalf("expected no error when admin_key_env not configured: %v", err)
	}
}

func TestValidateEnv_NoKeyEnvRefs(t *testing.T) {
	cfg := &Config{}
	if err := cfg.ValidateEnv(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateEnv_MultipleAgentsMixedPresence(t *testing.T) {
	t.Setenv("AGENT_A_KEY", "secret-a")
	os.Unsetenv("AGENT_B_KEY")
	cfg := &Config{
		Agents: []AgentConfig{
			{ID: "agent-a", KeyEnv: "AGENT_A_KEY"},
			{ID: "agent-b", KeyEnv: "AGENT_B_KEY"},
		},
	}
	err := cfg.ValidateEnv()
	if err == nil {
		t.Fatal("expected error for missing agent-b key_env")
	}
	if !strings.Contains(err.Error(), "agent-b") {
		t.Errorf("error should mention agent-b: %v", err)
	}
}

func TestLoadConfig_ConclaveCommandMissingPlaceholder(t *testing.T) {
	os.Setenv("TEST_CC_KEY", "test")
	t.Cleanup(func() { os.Unsetenv("TEST_CC_KEY") })

	yaml := `
conclaves:
  test:
    key_env: TEST_CC_KEY
    commands:
      bad:
        template: "echo hello"
        args:
          - name: missing
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for missing placeholder")
	}
	if !strings.Contains(err.Error(), "missing") {
		t.Errorf("error should mention missing placeholder: %v", err)
	}
}

func TestLoadConfig_UnknownKeyError(t *testing.T) {
	// "presets" is not a valid key (should be "preset")
	yaml := `
presets_dir: ../../presets
endpoints:
  github:
    presets: github
    upstream: https://api.github.com
    auth:
      type: bearer
      credential_env: GITHUB_TOKEN
    rules:
      - match: { method: GET }
        action: allow
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for unknown key 'presets'")
	}
}

// Sealed credential tests

func TestLoadConfig_SealedEndpointValid(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
  seal:
    key_env: WARDGATE_SEAL_KEY
endpoints:
  github:
    upstream: https://api.github.com
    auth:
      sealed: true
    rules:
      - match: { method: GET }
        action: allow
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["github"]
	if !ep.Auth.Sealed {
		t.Error("expected sealed to be true")
	}
	if ep.Auth.Type != "" {
		t.Errorf("expected empty auth type for sealed, got %s", ep.Auth.Type)
	}
}

func TestLoadConfig_SealedWithoutServerSeal(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
endpoints:
  github:
    upstream: https://api.github.com
    auth:
      sealed: true
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error when sealed without server.seal")
	}
	if !strings.Contains(err.Error(), "sealed requires server.seal") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadConfig_SealedWithCredentialEnv(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
  seal:
    key_env: WARDGATE_SEAL_KEY
endpoints:
  github:
    upstream: https://api.github.com
    auth:
      sealed: true
      credential_env: GITHUB_TOKEN
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error when sealed and credential_env both set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadConfig_SealMissingKeyEnv(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
  seal:
    cache_size: 500
endpoints:
  test:
    upstream: https://example.com
    auth:
      type: bearer
      credential_env: TEST_KEY
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for seal missing key_env")
	}
	if !strings.Contains(err.Error(), "key_env") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoadConfig_SealedEndpointWithCacheSize(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
  seal:
    key_env: WARDGATE_SEAL_KEY
    cache_size: 500
endpoints:
  github:
    upstream: https://api.github.com
    auth:
      sealed: true
    rules:
      - match: { method: GET }
        action: allow
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Seal.CacheSize != 500 {
		t.Errorf("expected cache_size 500, got %d", cfg.Server.Seal.CacheSize)
	}
}

func TestValidateEnv_SealKeyEnvMissing(t *testing.T) {
	os.Unsetenv("WARDGATE_NONEXISTENT_SEAL_KEY")
	cfg := &Config{
		Server: ServerConfig{
			Seal: &SealConfig{KeyEnv: "WARDGATE_NONEXISTENT_SEAL_KEY"},
		},
	}
	err := cfg.ValidateEnv()
	if err == nil {
		t.Fatal("expected error for missing seal key_env")
	}
	if !strings.Contains(err.Error(), "WARDGATE_NONEXISTENT_SEAL_KEY") {
		t.Errorf("error should mention env var name: %v", err)
	}
}

func TestValidateEnv_SealKeyEnvPresent(t *testing.T) {
	t.Setenv("TEST_SEAL_KEY", "abcd1234")
	cfg := &Config{
		Server: ServerConfig{
			Seal: &SealConfig{KeyEnv: "TEST_SEAL_KEY"},
		},
	}
	if err := cfg.ValidateEnv(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestLoadConfig_SealedWithAllowedHeaders(t *testing.T) {
	yaml := `
server:
  seal:
    key_env: TEST_SEAL_KEY
    allowed_headers:
      - Authorization
      - X-Custom-Auth

endpoints:
  github:
    upstream: https://api.github.com
    auth:
      sealed: true
    rules:
      - match: { method: GET }
        action: allow
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if cfg.Server.Seal == nil {
		t.Fatal("expected seal config")
	}

	if len(cfg.Server.Seal.AllowedHeaders) != 2 {
		t.Errorf("expected 2 allowed headers, got %d", len(cfg.Server.Seal.AllowedHeaders))
	}

	if cfg.Server.Seal.AllowedHeaders[0] != "Authorization" {
		t.Errorf("expected first header Authorization, got %s", cfg.Server.Seal.AllowedHeaders[0])
	}

	if cfg.Server.Seal.AllowedHeaders[1] != "X-Custom-Auth" {
		t.Errorf("expected second header X-Custom-Auth, got %s", cfg.Server.Seal.AllowedHeaders[1])
	}
}

func TestLoadConfig_BasicAuth(t *testing.T) {
	yaml := `
endpoints:
  my-api:
    upstream: https://api.example.com
    auth:
      type: basic
      credential_env: MY_CREDS
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["my-api"]
	if ep.Auth.Type != "basic" {
		t.Errorf("expected auth type 'basic', got %s", ep.Auth.Type)
	}
}

func TestLoadConfig_HeaderAuth(t *testing.T) {
	yaml := `
endpoints:
  bird-api:
    upstream: https://api.bird.com/v1
    auth:
      type: header
      header: Authorization
      prefix: "AccessKey "
      credential_env: BIRD_API_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["bird-api"]
	if ep.Auth.Type != "header" {
		t.Errorf("expected auth type 'header', got %s", ep.Auth.Type)
	}
	if ep.Auth.Header != "Authorization" {
		t.Errorf("expected header 'Authorization', got %s", ep.Auth.Header)
	}
	if ep.Auth.Prefix != "AccessKey " {
		t.Errorf("expected prefix 'AccessKey ', got %s", ep.Auth.Prefix)
	}
}

func TestLoadConfig_HeaderAuthMissingHeader(t *testing.T) {
	yaml := `
endpoints:
  bad:
    upstream: https://example.com
    auth:
      type: header
      credential_env: SOME_KEY
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for header auth without header field")
	}
	if !strings.Contains(err.Error(), "header") {
		t.Errorf("error should mention 'header': %v", err)
	}
}

func TestLoadConfig_HeaderAuthNoPrefix(t *testing.T) {
	yaml := `
endpoints:
  api:
    upstream: https://example.com
    auth:
      type: header
      header: X-Api-Key
      credential_env: SOME_KEY
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ep := cfg.Endpoints["api"]
	if ep.Auth.Prefix != "" {
		t.Errorf("expected empty prefix, got %q", ep.Auth.Prefix)
	}
}

func TestLoadConfig_CapabilitiesWithoutPreset(t *testing.T) {
	yaml := `
endpoints:
  custom:
    upstream: https://example.com/api
    auth:
      type: bearer
      credential_env: MY_TOKEN
    capabilities:
      read_data: allow
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for capabilities without preset")
	}
	if !strings.Contains(err.Error(), "capabilities") {
		t.Errorf("error should mention capabilities: %v", err)
	}
}

func TestLoadConfig_AllowedUpstreamsValid(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
agents:
  - id: tessa
    key_env: WARDGATE_AGENT_KEY
endpoints:
  google:
    allowed_upstreams:
      - "https://*.googleapis.com"
      - "https://storage.googleapis.com"
    auth:
      type: bearer
      credential_env: GOOGLE_KEY
    rules:
      - match: { method: GET }
        action: allow
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ep := cfg.Endpoints["google"]
	if len(ep.AllowedUpstreams) != 2 {
		t.Errorf("expected 2 allowed upstreams, got %d", len(ep.AllowedUpstreams))
	}
	if ep.Upstream != "" {
		t.Errorf("expected empty upstream, got %s", ep.Upstream)
	}
}

func TestLoadConfig_AllowedUpstreamsWithStatic(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
agents:
  - id: tessa
    key_env: WARDGATE_AGENT_KEY
endpoints:
  github:
    upstream: https://api.github.com
    allowed_upstreams:
      - "https://api.github.com"
      - "https://uploads.github.com"
    auth:
      type: bearer
      credential_env: GITHUB_KEY
    rules:
      - match: { method: GET }
        action: allow
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ep := cfg.Endpoints["github"]
	if ep.Upstream != "https://api.github.com" {
		t.Errorf("expected static upstream, got %s", ep.Upstream)
	}
	if len(ep.AllowedUpstreams) != 2 {
		t.Errorf("expected 2 allowed upstreams, got %d", len(ep.AllowedUpstreams))
	}
}

func TestLoadConfig_AllowedUpstreamsNoScheme(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
agents:
  - id: tessa
    key_env: WARDGATE_AGENT_KEY
endpoints:
  test:
    allowed_upstreams:
      - "*.example.com"
    auth:
      type: bearer
      credential_env: TEST_KEY
    rules:
      - match: { method: GET }
        action: allow
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for allowed_upstreams without scheme")
	}
	if !strings.Contains(err.Error(), "http://") {
		t.Errorf("error should mention scheme requirement: %v", err)
	}
}

func TestLoadConfig_AllowedUpstreamsNonHTTPAdapter(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
agents:
  - id: tessa
    key_env: WARDGATE_AGENT_KEY
endpoints:
  test:
    adapter: imap
    upstream: imaps://imap.example.com:993
    allowed_upstreams:
      - "https://api.example.com"
    auth:
      type: bearer
      credential_env: TEST_KEY
    imap:
      tls: true
    rules:
      - match: { method: GET }
        action: allow
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for allowed_upstreams on non-HTTP adapter")
	}
	if !strings.Contains(err.Error(), "only valid for HTTP") {
		t.Errorf("error should mention HTTP adapter requirement: %v", err)
	}
}

func TestLoadConfig_MissingBothUpstreams(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
agents:
  - id: tessa
    key_env: WARDGATE_AGENT_KEY
endpoints:
  test:
    auth:
      type: bearer
      credential_env: TEST_KEY
    rules:
      - match: { method: GET }
        action: allow
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error when neither upstream nor allowed_upstreams is set")
	}
	if !strings.Contains(err.Error(), "missing upstream") {
		t.Errorf("error should mention missing upstream: %v", err)
	}
}

func TestLoadConfig_TimeoutValid(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
agents:
  - id: tessa
    key_env: WARDGATE_AGENT_KEY
endpoints:
  openai:
    upstream: https://api.openai.com/v1
    timeout: "10m"
    auth:
      type: bearer
      credential_env: OPENAI_KEY
    rules:
      - match: { method: "*" }
        action: allow
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ep := cfg.Endpoints["openai"]
	if ep.Timeout != "10m" {
		t.Errorf("expected timeout '10m', got %s", ep.Timeout)
	}
}

func TestLoadConfig_TimeoutInvalid(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
agents:
  - id: tessa
    key_env: WARDGATE_AGENT_KEY
endpoints:
  test:
    upstream: https://api.example.com
    timeout: "invalid"
    auth:
      type: bearer
      credential_env: TEST_KEY
    rules:
      - match: { method: "*" }
        action: allow
`
	_, err := LoadFromReader(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected error for invalid timeout")
	}
	if !strings.Contains(err.Error(), "invalid timeout") {
		t.Errorf("error should mention invalid timeout: %v", err)
	}
}

func TestLoadConfig_SSEMode(t *testing.T) {
	yaml := `
server:
  listen: ":8080"
agents:
  - id: tessa
    key_env: WARDGATE_AGENT_KEY
endpoints:
  openai:
    upstream: https://api.openai.com/v1
    auth:
      type: bearer
      credential_env: OPENAI_KEY
    filter:
      enabled: true
      patterns: [api_keys]
      action: redact
      sse_mode: passthrough
    rules:
      - match: { method: "*" }
        action: allow
`
	cfg, err := LoadFromReader(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	ep := cfg.Endpoints["openai"]
	if ep.Filter == nil {
		t.Fatal("expected filter config")
	}
	if ep.Filter.SSEMode != "passthrough" {
		t.Errorf("expected sse_mode 'passthrough', got %s", ep.Filter.SSEMode)
	}
}
