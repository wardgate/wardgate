package config

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the root configuration structure.
type Config struct {
	Server    ServerConfig        `yaml:"server"`
	Agents    []AgentConfig       `yaml:"agents"`
	Endpoints map[string]Endpoint `yaml:"endpoints"`
	Notify    NotifyConfig        `yaml:"notify,omitempty"`
}

// ServerConfig holds server settings.
type ServerConfig struct {
	Listen      string `yaml:"listen"`
	ApprovalURL string `yaml:"approval_url,omitempty"` // Base URL for approval links
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
	Adapter  string     `yaml:"adapter,omitempty"` // "http" (default) or "imap"
	Upstream string     `yaml:"upstream"`
	Auth     AuthConfig `yaml:"auth"`
	Rules    []Rule     `yaml:"rules"`
	IMAP     *IMAPConfig `yaml:"imap,omitempty"` // IMAP-specific settings
}

// IMAPConfig holds IMAP-specific settings.
type IMAPConfig struct {
	TLS                bool `yaml:"tls"`
	InsecureSkipVerify bool `yaml:"insecure_skip_verify,omitempty"` // Skip TLS cert verification (for ProtonBridge)
	MaxConns           int  `yaml:"max_conns,omitempty"`            // Max connections per endpoint
	IdleTimeoutSecs    int  `yaml:"idle_timeout_secs,omitempty"`    // Idle connection timeout
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

	// Validate
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return &cfg, nil
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
