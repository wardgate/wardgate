package conclave

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ExecRequest is a command execution request from wardgate.
type ExecRequest struct {
	ID      string `json:"id"`
	Command string `json:"command"`
	Args    string `json:"args"`
	Cwd     string `json:"cwd"`
}

// ExecResult is the final result of a command execution.
type ExecResult struct {
	ID         string `json:"id"`
	Code       int    `json:"code"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// OutputChunk is a chunk of stdout or stderr from a running command.
type OutputChunk struct {
	ID     string
	Stream string // "stdout" or "stderr"
	Data   string
}

// Executor runs commands in the conclave environment.
type Executor struct {
	maxOutputBytes int64
	allowedBins    map[string]bool // absolute path -> allowed
}

// NewExecutor creates a new command executor.
func NewExecutor(cfg *Config) *Executor {
	allowed := make(map[string]bool, len(cfg.AllowedBins))
	for _, bin := range cfg.AllowedBins {
		allowed[bin] = true
	}
	return &Executor{
		maxOutputBytes: cfg.MaxOutputBytes,
		allowedBins:    allowed,
	}
}

// resolveCommand resolves a command name to an absolute path and checks
// it against the local allowlist (if configured).
func (e *Executor) resolveCommand(name string) (string, error) {
	var resolved string
	if filepath.IsAbs(name) {
		resolved = name
	} else {
		p, err := exec.LookPath(name)
		if err != nil {
			return "", fmt.Errorf("command not found: %s", name)
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", fmt.Errorf("cannot resolve path for %s: %w", name, err)
		}
		resolved = abs
	}

	// Enforce local allowlist if configured
	if len(e.allowedBins) > 0 && !e.allowedBins[resolved] {
		return "", fmt.Errorf("command %s (%s) not in local allowlist", name, resolved)
	}

	return resolved, nil
}

// Execute runs a command and streams output via the onOutput callback.
// It returns the exit result when the command completes.
func (e *Executor) Execute(ctx context.Context, req ExecRequest, onOutput func(OutputChunk)) ExecResult {
	start := time.Now()

	// Determine working directory (always provided by gateway; fallback to /)
	cwd := req.Cwd
	if cwd == "" {
		cwd = "/"
	}

	// Resolve the command binary
	resolved, err := e.resolveCommand(req.Command)
	if err != nil {
		return ExecResult{
			ID:         req.ID,
			Code:       -1,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      err.Error(),
		}
	}

	// Build the command. If there are args, use sh -c so that shell
	// features like pipes within the raw string work on the conclave side.
	var cmd *exec.Cmd
	if req.Args != "" {
		cmd = exec.CommandContext(ctx, "sh", "-c", resolved+" "+req.Args)
	} else {
		cmd = exec.CommandContext(ctx, resolved)
	}
	cmd.Dir = cwd

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return ExecResult{
			ID:         req.ID,
			Code:       -1,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      fmt.Sprintf("stdout pipe: %v", err),
		}
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return ExecResult{
			ID:         req.ID,
			Code:       -1,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      fmt.Sprintf("stderr pipe: %v", err),
		}
	}

	if err := cmd.Start(); err != nil {
		return ExecResult{
			ID:         req.ID,
			Code:       -1,
			DurationMs: time.Since(start).Milliseconds(),
			Error:      fmt.Sprintf("start: %v", err),
		}
	}

	// Stream stdout and stderr concurrently with output limit enforcement.
	var totalBytes int64
	var limitExceeded bool
	var mu sync.Mutex

	var wg sync.WaitGroup
	wg.Add(2)

	streamFn := func(stream string, r io.Reader) {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				mu.Lock()
				totalBytes += int64(n)
				exceeded := totalBytes > e.maxOutputBytes
				mu.Unlock()

				if exceeded {
					mu.Lock()
					limitExceeded = true
					mu.Unlock()
					// Kill the process — we're over the limit
					cmd.Process.Kill()
					return
				}

				onOutput(OutputChunk{
					ID:     req.ID,
					Stream: stream,
					Data:   string(buf[:n]),
				})
			}
			if err != nil {
				return
			}
		}
	}

	go streamFn("stdout", stdout)
	go streamFn("stderr", stderr)
	wg.Wait()

	exitCode := 0
	waitErr := cmd.Wait()
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	result := ExecResult{
		ID:         req.ID,
		Code:       exitCode,
		DurationMs: time.Since(start).Milliseconds(),
	}

	if limitExceeded {
		result.Error = "output_limit_exceeded"
		result.Code = -1
	}

	return result
}

// CheckCommand validates a command without executing it.
// Returns the resolved absolute path or an error.
func (e *Executor) CheckCommand(name string) (string, error) {
	return e.resolveCommand(name)
}

// AllowlistSummary returns a human-readable summary of the allowlist.
func (e *Executor) AllowlistSummary() string {
	if len(e.allowedBins) == 0 {
		return "no local allowlist (all commands deferred to wardgate policy)"
	}
	bins := make([]string, 0, len(e.allowedBins))
	for bin := range e.allowedBins {
		bins = append(bins, bin)
	}
	return fmt.Sprintf("allowed: %s", strings.Join(bins, ", "))
}
