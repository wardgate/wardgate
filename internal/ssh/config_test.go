package ssh

import (
	"testing"
)

func TestParsePrivateKey(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "empty key",
			input:   "",
			wantErr: true,
		},
		{
			name:    "invalid key",
			input:   "not a key",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParsePrivateKey([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParsePrivateKey() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDefaultConnectionConfig(t *testing.T) {
	cfg := ConnectionConfig{
		Host:     "example.com",
		Username: "deploy",
	}

	cfg = applyDefaults(cfg)

	if cfg.Port != 22 {
		t.Errorf("expected default port 22, got %d", cfg.Port)
	}
	if cfg.MaxSessions != 5 {
		t.Errorf("expected default max sessions 5, got %d", cfg.MaxSessions)
	}
	if cfg.TimeoutSecs != 30 {
		t.Errorf("expected default timeout 30, got %d", cfg.TimeoutSecs)
	}
}

func TestDefaultConnectionConfig_NoOverride(t *testing.T) {
	cfg := ConnectionConfig{
		Host:        "example.com",
		Port:        2222,
		Username:    "deploy",
		MaxSessions: 10,
		TimeoutSecs: 60,
	}

	cfg = applyDefaults(cfg)

	if cfg.Port != 2222 {
		t.Errorf("expected port 2222, got %d", cfg.Port)
	}
	if cfg.MaxSessions != 10 {
		t.Errorf("expected max sessions 10, got %d", cfg.MaxSessions)
	}
	if cfg.TimeoutSecs != 60 {
		t.Errorf("expected timeout 60, got %d", cfg.TimeoutSecs)
	}
}
