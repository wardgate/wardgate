package cli

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// Config holds wardgate-cli configuration.
type Config struct {
	Server string `yaml:"server"`
	Key    string `yaml:"key"`
	KeyEnv string `yaml:"key_env"`
}

// Load loads config from the given paths. Env file is loaded first (if path
// is non-empty and file exists) to populate key_env lookup, then config YAML.
// Server and key come from config only-env vars do NOT override, so the agent
// cannot redirect to arbitrary URLs by setting WARDGATE_URL.
func Load(envPath, configPath string) (*Config, error) {
	cfg := &Config{}

	// Load env file first (for key_env resolution only)
	if envPath != "" {
		_ = godotenv.Load(envPath) // ignore error if file doesn't exist
	}

	// Load config YAML - this is the source of truth for server and key
	if configPath != "" {
		if _, err := os.Stat(configPath); err == nil {
			data, err := os.ReadFile(configPath)
			if err != nil {
				return nil, fmt.Errorf("read config: %w", err)
			}
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, fmt.Errorf("parse config: %w", err)
			}
		}
	}

	// Resolve key from key_env if set (reads env populated by -env file or process env)
	if cfg.Key == "" && cfg.KeyEnv != "" {
		cfg.Key = os.Getenv(cfg.KeyEnv)
	}

	return cfg, nil
}

// GetKey returns the agent key from config (key or key_env).
func (c *Config) GetKey() (string, error) {
	if c.Key != "" {
		return c.Key, nil
	}
	if c.KeyEnv != "" {
		k := os.Getenv(c.KeyEnv)
		if k == "" {
			return "", fmt.Errorf("key not set: %s (set in -env file or process env)", c.KeyEnv)
		}
		return k, nil
	}
	return "", fmt.Errorf("key not set: configure key or key_env in config file")
}
