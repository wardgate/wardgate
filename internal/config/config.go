package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// PresetInfo describes an available API preset for easy configuration.
type PresetInfo struct {
	Name         string       // e.g., "todoist"
	Description  string       // Human-readable description
	Upstream     string       // Default upstream URL
	AuthType     string       // Default auth type (e.g., "bearer")
	Adapter      string       // Adapter type (e.g., "imap", "smtp") - empty means http
	Capabilities []Capability // Available capabilities for this preset
}

// Capability defines a named, user-friendly permission that maps to rules.
type Capability struct {
	Name        string // e.g., "create_issues"
	Description string // Human-readable description, e.g., "Create new issues"
	Rules       []Rule // Rules to apply when this capability is enabled
}

// LoadPresetsFromDir loads all presets from a directory.
// This can be used to get available presets for documentation or CLI tools.
func LoadPresetsFromDir(dir string) ([]PresetInfo, error) {
	presets, err := loadExternalPresets(dir)
	if err != nil {
		return nil, err
	}
	result := make([]PresetInfo, 0, len(presets))
	for _, p := range presets {
		result = append(result, p)
	}
	return result, nil
}

// loadExternalPresets loads preset files from a directory.
func loadExternalPresets(dir string) (map[string]PresetInfo, error) {
	presets := make(map[string]PresetInfo)

	if dir == "" {
		return presets, nil
	}

	// Check if directory exists
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		// Directory doesn't exist - not an error, just return empty
		return presets, nil
	}
	if err != nil {
		return nil, fmt.Errorf("checking presets_dir: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("presets_dir %q is not a directory", dir)
	}

	// Read all YAML files in directory
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading presets_dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		path := filepath.Join(dir, name)
		preset, err := loadPresetFile(path)
		if err != nil {
			return nil, fmt.Errorf("loading preset %s: %w", path, err)
		}

		presets[preset.Name] = preset
	}

	return presets, nil
}

// loadPresetFile loads a single preset from a YAML file.
func loadPresetFile(path string) (PresetInfo, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return PresetInfo{}, err
	}

	var def CustomPresetDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return PresetInfo{}, fmt.Errorf("parsing yaml: %w", err)
	}

	// Use filename (without extension) as name if not specified
	if def.Name == "" {
		base := filepath.Base(path)
		def.Name = strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
	}

	return customPresetDefToPresetInfo(def), nil
}

// customPresetDefToPresetInfo converts a CustomPresetDef to PresetInfo.
func customPresetDefToPresetInfo(def CustomPresetDef) PresetInfo {
	caps := make([]Capability, len(def.Capabilities))
	for i, capDef := range def.Capabilities {
		caps[i] = Capability{
			Name:        capDef.Name,
			Description: capDef.Description,
			Rules:       capDef.Rules,
		}
	}

	return PresetInfo{
		Name:         def.Name,
		Description:  def.Description,
		Upstream:     def.Upstream,
		AuthType:     def.AuthType,
		Adapter:      def.Adapter,
		Capabilities: caps,
	}
}

// buildPresetRegistry creates a merged preset registry with correct priority.
// Priority: inline custom > external files (presets_dir)
func (c *Config) buildPresetRegistry() (map[string]PresetInfo, error) {
	registry := make(map[string]PresetInfo)

	// Load external presets from presets_dir
	external, err := loadExternalPresets(c.PresetsDir)
	if err != nil {
		return nil, err
	}
	for name, preset := range external {
		registry[name] = preset
	}

	// Apply inline custom presets (highest priority, override external)
	for name, def := range c.CustomPresets {
		if def.Name == "" {
			def.Name = name
		}
		registry[name] = customPresetDefToPresetInfo(def)
	}

	return registry, nil
}

// Config is the root configuration structure.
type Config struct {
	Server        ServerConfig                `yaml:"server"`
	Agents        []AgentConfig               `yaml:"agents"`
	Endpoints     map[string]Endpoint         `yaml:"endpoints"`
	Notify        NotifyConfig                `yaml:"notify,omitempty"`
	PresetsDir    string                      `yaml:"presets_dir,omitempty"`    // Directory containing custom preset YAML files
	CustomPresets map[string]CustomPresetDef  `yaml:"custom_presets,omitempty"` // Inline custom preset definitions
}

// CustomPresetDef defines a user-created preset in YAML.
type CustomPresetDef struct {
	Name         string          `yaml:"name,omitempty"`    // Optional, can use map key
	Description  string          `yaml:"description"`
	Upstream     string          `yaml:"upstream"`
	AuthType     string          `yaml:"auth_type"`
	Adapter      string          `yaml:"adapter,omitempty"` // "imap", "smtp", or empty for http
	Capabilities []CapabilityDef `yaml:"capabilities,omitempty"`
}

// CapabilityDef defines a capability in a custom preset.
type CapabilityDef struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Rules       []Rule `yaml:"rules"`
}

// ServerConfig holds server settings.
type ServerConfig struct {
	Listen      string `yaml:"listen"`
	BaseURL     string `yaml:"base_url,omitempty"`      // Base URL for links in notifications
	AdminKeyEnv string `yaml:"admin_key_env,omitempty"` // Env var for admin key (for web UI/CLI)
}

// NotifyConfig holds notification settings.
type NotifyConfig struct {
	Webhook *WebhookNotifyConfig `yaml:"webhook,omitempty"`
	Slack   *SlackNotifyConfig   `yaml:"slack,omitempty"`
	Timeout string               `yaml:"timeout,omitempty"` // Approval timeout (e.g., "5m")
}

// WebhookNotifyConfig configures a generic webhook notifier.
type WebhookNotifyConfig struct {
	URL     string            `yaml:"url"`
	Headers map[string]string `yaml:"headers,omitempty"`
}

// SlackNotifyConfig configures Slack notifications.
type SlackNotifyConfig struct {
	WebhookURL string `yaml:"webhook_url"`
}

// AgentConfig defines an agent that can access the gateway.
type AgentConfig struct {
	ID     string `yaml:"id"`
	KeyEnv string `yaml:"key_env"`
}

// Endpoint defines a proxied service.
type Endpoint struct {
	Preset       string            `yaml:"preset,omitempty"`       // Preset name (e.g., "todoist", "github")
	Description  string            `yaml:"description,omitempty"`  // User-friendly description for discovery API
	Adapter      string            `yaml:"adapter,omitempty"`      // "http" (default), "imap", or "smtp"
	Upstream     string            `yaml:"upstream,omitempty"`
	Auth         AuthConfig        `yaml:"auth"`
	Capabilities map[string]string `yaml:"capabilities,omitempty"` // Named capabilities with actions (e.g., "create_issues": "allow")
	Rules        []Rule            `yaml:"rules,omitempty"`
	IMAP         *IMAPConfig       `yaml:"imap,omitempty"` // IMAP-specific settings
	SMTP         *SMTPConfig       `yaml:"smtp,omitempty"` // SMTP-specific settings
}

// IMAPConfig holds IMAP-specific settings.
type IMAPConfig struct {
	TLS                bool `yaml:"tls"`
	InsecureSkipVerify bool `yaml:"insecure_skip_verify,omitempty"` // Skip TLS cert verification (for ProtonBridge)
	MaxConns           int  `yaml:"max_conns,omitempty"`            // Max connections per endpoint
	IdleTimeoutSecs    int  `yaml:"idle_timeout_secs,omitempty"`    // Idle connection timeout
}

// SMTPConfig holds SMTP-specific settings.
type SMTPConfig struct {
	TLS                bool     `yaml:"tls"`                            // Use implicit TLS (port 465)
	StartTLS           bool     `yaml:"starttls,omitempty"`             // Use STARTTLS (port 587)
	InsecureSkipVerify bool     `yaml:"insecure_skip_verify,omitempty"` // Skip TLS cert verification (for self-signed)
	From               string   `yaml:"from,omitempty"`                 // Default from address
	AllowedRecipients  []string `yaml:"allowed_recipients,omitempty"`   // Allowlist of recipients (email or @domain)
	KnownRecipients    []string `yaml:"known_recipients,omitempty"`     // Known recipients that don't need approval
	AskNewRecipients   bool     `yaml:"ask_new_recipients,omitempty"`   // Ask before sending to new recipients
	BlockedKeywords    []string `yaml:"blocked_keywords,omitempty"`     // Keywords to block in subject/body
}

// AuthConfig defines how to authenticate to the upstream.
type AuthConfig struct {
	Type          string `yaml:"type"`
	CredentialEnv string `yaml:"credential_env"`
}

// Rule defines a policy rule for an endpoint.
type Rule struct {
	Match     Match      `yaml:"match"`
	Action    string     `yaml:"action"`
	Message   string     `yaml:"message,omitempty"`
	RateLimit *RateLimit `yaml:"rate_limit,omitempty"`
	TimeRange *TimeRange `yaml:"time_range,omitempty"`
}

// Match defines the conditions for a rule to apply.
type Match struct {
	Method string `yaml:"method,omitempty"`
	Path   string `yaml:"path,omitempty"`
}

// RateLimit defines rate limiting for a rule.
type RateLimit struct {
	Max    int    `yaml:"max"`              // Maximum requests
	Window string `yaml:"window,omitempty"` // Time window (e.g., "1m", "1h")
}

// TimeRange defines when a rule is active.
type TimeRange struct {
	Hours []string `yaml:"hours,omitempty"` // e.g., ["09:00-17:00"]
	Days  []string `yaml:"days,omitempty"`  // e.g., ["mon", "tue", "wed", "thu", "fri"]
}

// validActions are the allowed action types.
var validActions = map[string]bool{
	"allow": true,
	"deny":  true,
	"ask":   true,
	"queue": true,
}

// LoadFromFile loads configuration from a YAML file.
func LoadFromFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config file: %w", err)
	}
	defer f.Close()
	return LoadFromReader(f)
}

// LoadFromReader loads configuration from an io.Reader.
func LoadFromReader(r io.Reader) (*Config, error) {
	var cfg Config
	decoder := yaml.NewDecoder(r)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	// Apply defaults
	if cfg.Server.Listen == "" {
		cfg.Server.Listen = ":8080"
	}

	// Apply presets to endpoints
	if err := cfg.applyPresets(); err != nil {
		return nil, err
	}

	// Validate
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
}

// applyPresets applies presets (builtin, external, and inline) to endpoints.
func (c *Config) applyPresets() error {
	// Build merged preset registry
	registry, err := c.buildPresetRegistry()
	if err != nil {
		return err
	}

	for name, ep := range c.Endpoints {
		if ep.Preset == "" {
			continue
		}

		preset, ok := registry[ep.Preset]
		if !ok {
			return fmt.Errorf("endpoint %q: unknown preset %q", name, ep.Preset)
		}

		// Apply preset defaults (user config overrides preset)
		if ep.Upstream == "" {
			ep.Upstream = preset.Upstream
		}
		if ep.Auth.Type == "" {
			ep.Auth.Type = preset.AuthType
		}
		if ep.Adapter == "" && preset.Adapter != "" {
			ep.Adapter = preset.Adapter
		}

		// If capabilities are specified, expand them into rules
		if len(ep.Capabilities) > 0 {
			rules, err := expandCapabilities(name, preset, ep.Capabilities)
			if err != nil {
				return err
			}
			ep.Rules = rules
		} else if len(ep.Rules) == 0 {
			// No capabilities and no custom rules - default to deny all
			ep.Rules = []Rule{{
				Match:   Match{Method: "*"},
				Action:  "deny",
				Message: "No capabilities configured",
			}}
		}

		// Update the endpoint in the map
		c.Endpoints[name] = ep
	}
	return nil
}

// expandCapabilities converts user-specified capabilities into rules.
func expandCapabilities(endpointName string, preset PresetInfo, capabilities map[string]string) ([]Rule, error) {
	// Build a map of capability name to capability definition
	capMap := make(map[string]Capability)
	for _, cap := range preset.Capabilities {
		capMap[cap.Name] = cap
	}

	var rules []Rule
	for capName, action := range capabilities {
		cap, ok := capMap[capName]
		if !ok {
			return nil, fmt.Errorf("endpoint %q: unknown capability %q for preset %q", endpointName, capName, preset.Name)
		}

		// Validate action
		if !validActions[action] {
			return nil, fmt.Errorf("endpoint %q: invalid action %q for capability %q", endpointName, action, capName)
		}

		// Clone rules from capability and apply the user's action
		for _, rule := range cap.Rules {
			newRule := Rule{
				Match:   rule.Match,
				Action:  action,
				Message: rule.Message,
			}
			rules = append(rules, newRule)
		}
	}

	// Add a catch-all deny rule at the end
	rules = append(rules, Rule{
		Match:   Match{Method: "*"},
		Action:  "deny",
		Message: "Operation not permitted",
	})

	return rules, nil
}

// GetEndpointDescription returns the description for an endpoint.
// Priority: endpoint.Description > preset.Description > adapter name
func (c *Config) GetEndpointDescription(name string, ep Endpoint) string {
	// 1. Use explicit description if set
	if ep.Description != "" {
		return ep.Description
	}

	// 2. Use preset description if available
	if ep.Preset != "" {
		registry, err := c.buildPresetRegistry()
		if err == nil {
			if preset, ok := registry[ep.Preset]; ok && preset.Description != "" {
				return preset.Description
			}
		}
	}

	// 3. Fallback to adapter name
	adapter := strings.ToUpper(ep.Adapter)
	if adapter == "" {
		adapter = "HTTP"
	}
	return adapter
}

func (c *Config) validate() error {
	for name, ep := range c.Endpoints {
		if ep.Upstream == "" {
			return fmt.Errorf("endpoint %q: missing upstream", name)
		}
		if ep.Auth.Type == "" || ep.Auth.CredentialEnv == "" {
			return fmt.Errorf("endpoint %q: missing auth configuration", name)
		}
		for i, rule := range ep.Rules {
			if rule.Action == "" {
				continue
			}
			if !validActions[rule.Action] {
				return fmt.Errorf("endpoint %q rule %d: invalid action %q", name, i, rule.Action)
			}
		}
	}
	return nil
}
