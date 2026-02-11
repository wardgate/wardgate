package conclave

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Config is the configuration for wardgate-exec.
type Config struct {
	Server         string   `yaml:"server"`                     // WebSocket URL (wss://wardgate.example.com/conclaves/ws)
	Key            string   `yaml:"key"`                        // Conclave authentication key
	Name           string   `yaml:"name"`                       // Conclave name
	MaxInputBytes  int64    `yaml:"max_input_bytes,omitempty"`  // Max bytes for command input (default: 1MB)
	MaxOutputBytes int64    `yaml:"max_output_bytes,omitempty"` // Max bytes for command output (default: 10MB)
	AllowedBins    []string `yaml:"allowed_bins,omitempty"`     // Local binary allowlist (defense in depth)
}

const (
	DefaultMaxInputBytes  = 1 << 20  // 1MB
	DefaultMaxOutputBytes = 10 << 20 // 10MB
)

// LoadConfig loads wardgate-exec configuration from a YAML file.
func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) validate() error {
	if c.Server == "" {
		return fmt.Errorf("config: server is required")
	}
	if c.Key == "" {
		return fmt.Errorf("config: key is required")
	}
	if c.Name == "" {
		return fmt.Errorf("config: name is required")
	}
	return nil
}

func (c *Config) applyDefaults() {
	if c.MaxInputBytes <= 0 {
		c.MaxInputBytes = DefaultMaxInputBytes
	}
	if c.MaxOutputBytes <= 0 {
		c.MaxOutputBytes = DefaultMaxOutputBytes
	}
}
