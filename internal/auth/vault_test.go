package auth

import (
	"os"
	"testing"
)

func TestEnvVault_Get(t *testing.T) {
	os.Setenv("WARDGATE_CRED_TEST_KEY", "secret123")
	defer os.Unsetenv("WARDGATE_CRED_TEST_KEY")

	vault := NewEnvVault()
	cred, err := vault.Get("WARDGATE_CRED_TEST_KEY")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cred != "secret123" {
		t.Errorf("expected 'secret123', got '%s'", cred)
	}
}

func TestEnvVault_Get_NotSet(t *testing.T) {
	os.Unsetenv("WARDGATE_CRED_NONEXISTENT")

	vault := NewEnvVault()
	_, err := vault.Get("WARDGATE_CRED_NONEXISTENT")
	if err == nil {
		t.Fatal("expected error for missing env var")
	}
}

func TestVaultInterface(t *testing.T) {
	// Verify EnvVault implements Vault interface
	var _ Vault = (*EnvVault)(nil)
}

// MockVault for testing other components
type MockVault struct {
	Credentials map[string]string
}

func (m *MockVault) Get(name string) (string, error) {
	if cred, ok := m.Credentials[name]; ok {
		return cred, nil
	}
	return "", ErrCredentialNotFound
}

func TestMockVault_ImplementsInterface(t *testing.T) {
	var _ Vault = (*MockVault)(nil)
}
