package cli

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_NoEnvOverride(t *testing.T) {
	// Env vars must NOT override config - agent could set WARDGATE_URL to redirect elsewhere
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(configPath, []byte(`
server: http://config-server:8080
key: config-key-123
`), 0644)

	envPath := filepath.Join(dir, ".env")
	os.WriteFile(envPath, []byte(`
WARDGATE_URL=http://evil.com
WARDGATE_AGENT_KEY=env-key-123
`), 0644)

	cfg, err := Load(envPath, configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Server != "http://config-server:8080" {
		t.Errorf("server must come from config only, got %q", cfg.Server)
	}
	if cfg.Key != "config-key-123" {
		t.Errorf("key must come from config only, got %q", cfg.Key)
	}
}

func TestLoad_KeyEnv(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(configPath, []byte(`
server: http://localhost:8080
key_env: WARDGATE_AGENT_KEY
`), 0644)

	os.Setenv("WARDGATE_AGENT_KEY", "test-key-from-env")
	defer os.Unsetenv("WARDGATE_AGENT_KEY")

	cfg, err := Load("", configPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	key, err := cfg.GetKey()
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	if key != "test-key-from-env" {
		t.Errorf("expected key from key_env, got %q", key)
	}
}

func TestLoadRootCAs_Empty(t *testing.T) {
	cfg := &Config{}
	pool, err := cfg.LoadRootCAs()
	if err != nil {
		t.Fatalf("LoadRootCAs: %v", err)
	}
	if pool != nil {
		t.Error("expected nil pool when ca_file empty")
	}
}

func TestLoadRootCAs_NoFile(t *testing.T) {
	cfg := &Config{CAFile: "/nonexistent/ca.pem"}
	_, err := cfg.LoadRootCAs()
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func TestLoadRootCAs_Valid(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "Test CA"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageCertSign,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("CreateCertificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	dir := t.TempDir()
	caPath := filepath.Join(dir, "ca.pem")
	if err := os.WriteFile(caPath, certPEM, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg := &Config{CAFile: caPath}
	pool, err := cfg.LoadRootCAs()
	if err != nil {
		t.Fatalf("LoadRootCAs: %v", err)
	}
	if pool == nil {
		t.Error("expected non-nil pool")
	}
}
