package auth

import (
	"errors"
	"os"
)

// ErrCredentialNotFound is returned when a credential is not found.
var ErrCredentialNotFound = errors.New("credential not found")

// Vault is the interface for credential storage.
type Vault interface {
	Get(name string) (string, error)
}

// EnvVault retrieves credentials from environment variables.
type EnvVault struct{}

// NewEnvVault creates a new EnvVault.
func NewEnvVault() *EnvVault {
	return &EnvVault{}
}

// Get retrieves a credential from an environment variable.
func (v *EnvVault) Get(name string) (string, error) {
	val := os.Getenv(name)
	if val == "" {
		return "", ErrCredentialNotFound
	}
	return val, nil
}
