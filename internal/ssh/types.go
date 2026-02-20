package ssh

import (
	"context"
	"errors"

	"github.com/wardgate/wardgate/internal/approval"
)

var (
	ErrConnectionFailed  = errors.New("failed to connect to SSH server")
	ErrAuthFailed        = errors.New("SSH authentication failed")
	ErrHostKeyMismatch   = errors.New("SSH host key verification failed")
	ErrExecFailed        = errors.New("command execution failed")
	ErrMaxConnsReached   = errors.New("maximum connections reached")
	ErrConnectionTimeout = errors.New("connection timeout")
)

// ExecRequest is the JSON body for POST /exec.
type ExecRequest struct {
	Command string `json:"command"`
	Cwd     string `json:"cwd,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

// ExecResponse is the JSON response for exec requests.
type ExecResponse struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// ConnectionConfig holds SSH connection parameters.
type ConnectionConfig struct {
	Host               string
	Port               int
	Username           string
	PrivateKey         []byte
	KnownHost          string
	KnownHostsFile     string
	InsecureSkipVerify bool
	MaxSessions        int
	TimeoutSecs        int
}

// Client is the interface for executing commands via SSH.
type Client interface {
	Exec(ctx context.Context, command string, cwd string) (stdout, stderr string, exitCode int, err error)
	Close() error
	IsAlive() bool
}

// ApprovalRequester is the interface for requesting approval.
type ApprovalRequester interface {
	RequestApproval(ctx context.Context, endpoint, method, path, agentID string) (bool, error)
	RequestApprovalWithContent(ctx context.Context, req approval.ApprovalRequest) (bool, error)
}
