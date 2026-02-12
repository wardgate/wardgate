package conclave

import (
	"crypto/x509"
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
	CAFile         string   `yaml:"ca_file,omitempty"`          // Path to custom CA cert (PEM) for self-signed CAs
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

// LoadRootCAs returns a cert pool with system certs plus the custom CA from ca_file (if set).
// Returns nil when ca_file is empty (use system default).
func (c *Config) LoadRootCAs() (*x509.CertPool, error) {
	if c.CAFile == "" {
		return nil, nil
	}
	caPem, err := os.ReadFile(c.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read ca_file %s: %w", c.CAFile, err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(caPem) {
		return nil, fmt.Errorf("ca_file %s: no valid PEM certificates", c.CAFile)
	}
	return pool, nil
}

func (c *Config) applyDefaults() {
	if c.MaxInputBytes <= 0 {
		c.MaxInputBytes = DefaultMaxInputBytes
	}
	if c.MaxOutputBytes <= 0 {
		c.MaxOutputBytes = DefaultMaxOutputBytes
	}
}
