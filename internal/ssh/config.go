package ssh

import (
	"fmt"

	gossh "golang.org/x/crypto/ssh"
)

// ParsePrivateKey parses a PEM-encoded SSH private key.
func ParsePrivateKey(pemBytes []byte) (gossh.Signer, error) {
	if len(pemBytes) == 0 {
		return nil, fmt.Errorf("empty private key")
	}
	signer, err := gossh.ParsePrivateKey(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key: %w", err)
	}
	return signer, nil
}

// applyDefaults fills in zero-value fields with sensible defaults.
func applyDefaults(cfg ConnectionConfig) ConnectionConfig {
	if cfg.Port == 0 {
		cfg.Port = 22
	}
	if cfg.MaxSessions == 0 {
		cfg.MaxSessions = 5
	}
	if cfg.TimeoutSecs == 0 {
		cfg.TimeoutSecs = 30
	}
	return cfg
}
