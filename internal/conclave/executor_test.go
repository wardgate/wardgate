package conclave

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func newTestExecutor(allowedBins []string) *Executor {
	cfg := &Config{
		MaxOutputBytes: DefaultMaxOutputBytes,
		AllowedBins:    allowedBins,
	}
	return NewExecutor(cfg)
}

func TestExecutor_ResolveCommand(t *testing.T) {
	e := newTestExecutor(nil)

	resolved, err := e.CheckCommand("echo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(resolved) {
		t.Errorf("expected absolute path, got %q", resolved)
	}
}

func TestExecutor_ResolveCommand_NotFound(t *testing.T) {
	e := newTestExecutor(nil)

	_, err := e.CheckCommand("nonexistent_command_xyz_12345")
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}

func TestExecutor_ResolveCommand_AbsolutePath(t *testing.T) {
	e := newTestExecutor(nil)

	resolved, err := e.CheckCommand("/bin/echo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resolved != "/bin/echo" {
		t.Errorf("expected '/bin/echo', got %q", resolved)
	}
}

func TestExecutor_Allowlist_Allowed(t *testing.T) {
	// Find the absolute path of echo for the allowlist
	echoPath, err := exec.LookPath("echo")
	if err != nil {
		t.Skip("echo not found in PATH")
	}
	absPath, _ := filepath.Abs(echoPath)

	e := newTestExecutor([]string{absPath})

	resolved, err := e.CheckCommand("echo")
	if err != nil {
		t.Fatalf("expected echo to be allowed, got error: %v", err)
	}
	if resolved != absPath {
		t.Errorf("expected %q, got %q", absPath, resolved)
	}
}

func TestExecutor_Allowlist_Blocked(t *testing.T) {
	// Allowlist only has a dummy entry, not echo's real path
	e := newTestExecutor([]string{"/usr/bin/rg"})

	_, err := e.CheckCommand("echo")
	if err == nil {
		t.Fatal("expected error for command not in allowlist")
	}
	if !strings.Contains(err.Error(), "allowlist") {
		t.Errorf("expected allowlist error, got: %v", err)
	}
}

func TestExecutor_Execute_SimpleCommand(t *testing.T) {
	e := newTestExecutor(nil)

	var chunks []OutputChunk
	result := e.Execute(context.Background(), ExecRequest{
		ID:      "test-1",
		Command: "echo",
		Args:    "hello world",
		Cwd:     "/tmp",
	}, func(chunk OutputChunk) {
		chunks = append(chunks, chunk)
	})

	if result.Code != 0 {
		t.Errorf("expected exit code 0, got %d (error: %s)", result.Code, result.Error)
	}
	if result.ID != "test-1" {
		t.Errorf("expected ID 'test-1', got %q", result.ID)
	}

	var stdout strings.Builder
	for _, c := range chunks {
		if c.Stream == "stdout" {
			stdout.WriteString(c.Data)
		}
	}
	if !strings.Contains(stdout.String(), "hello world") {
		t.Errorf("expected stdout to contain 'hello world', got %q", stdout.String())
	}
}

func TestExecutor_Execute_FailingCommand(t *testing.T) {
	e := newTestExecutor(nil)

	result := e.Execute(context.Background(), ExecRequest{
		ID:      "test-2",
		Command: "false",
		Cwd:     "/tmp",
	}, func(chunk OutputChunk) {})

	if result.Code == 0 {
		t.Error("expected non-zero exit code for 'false'")
	}
}

func TestExecutor_Execute_WithCwdOverride(t *testing.T) {
	e := newTestExecutor(nil)

	var chunks []OutputChunk
	result := e.Execute(context.Background(), ExecRequest{
		ID:      "test-3",
		Command: "pwd",
		Cwd:     "/tmp",
	}, func(chunk OutputChunk) {
		chunks = append(chunks, chunk)
	})

	if result.Code != 0 {
		t.Errorf("expected exit code 0, got %d (error: %s)", result.Code, result.Error)
	}

	var stdout strings.Builder
	for _, c := range chunks {
		if c.Stream == "stdout" {
			stdout.WriteString(c.Data)
		}
	}
	// On macOS /tmp is a symlink to /private/tmp, so check for either
	out := strings.TrimSpace(stdout.String())
	if out != "/tmp" && out != "/private/tmp" {
		t.Errorf("expected cwd '/tmp' (or '/private/tmp'), got %q", out)
	}
}

func TestExecutor_Execute_UnknownCommand(t *testing.T) {
	e := newTestExecutor(nil)

	result := e.Execute(context.Background(), ExecRequest{
		ID:      "test-4",
		Command: "nonexistent_command_xyz_12345",
		Cwd:     "/tmp",
	}, func(chunk OutputChunk) {})

	if result.Code != -1 {
		t.Errorf("expected exit code -1 for unknown command, got %d", result.Code)
	}
	if result.Error == "" {
		t.Error("expected error message for unknown command")
	}
}

func TestExecutor_Execute_Cancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	e := newTestExecutor(nil)

	result := e.Execute(ctx, ExecRequest{
		ID:      "test-5",
		Command: "sleep",
		Args:    "60",
		Cwd:     "/tmp",
	}, func(chunk OutputChunk) {})

	// Should fail because context was cancelled
	if result.Code == 0 {
		t.Error("expected non-zero exit code for cancelled command")
	}
}

func TestExecutor_AllowlistSummary(t *testing.T) {
	e := newTestExecutor(nil)
	summary := e.AllowlistSummary()
	if !strings.Contains(summary, "no local allowlist") {
		t.Errorf("expected 'no local allowlist' for empty allowlist, got %q", summary)
	}

	e2 := newTestExecutor([]string{"/usr/bin/cat", "/usr/bin/rg"})
	summary2 := e2.AllowlistSummary()
	if !strings.Contains(summary2, "allowed:") {
		t.Errorf("expected 'allowed:' for non-empty allowlist, got %q", summary2)
	}
}
