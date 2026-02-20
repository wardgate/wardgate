package ssh

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// SSHClient is a real SSH client implementation.
type SSHClient struct {
	conn   *gossh.Client
	mu     sync.Mutex
	closed bool
}

// NewSSHClient creates a new SSH client connected to the remote host.
func NewSSHClient(cfg ConnectionConfig) (*SSHClient, error) {
	cfg = applyDefaults(cfg)

	signer, err := ParsePrivateKey(cfg.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("parsing SSH key: %w", err)
	}

	var hostKeyCallback gossh.HostKeyCallback
	switch {
	case cfg.InsecureSkipVerify:
		hostKeyCallback = gossh.InsecureIgnoreHostKey()
	case cfg.KnownHostsFile != "":
		hostKeyCallback, err = knownhosts.New(cfg.KnownHostsFile)
		if err != nil {
			return nil, fmt.Errorf("loading known_hosts: %w", err)
		}
	case cfg.KnownHost != "":
		hostKeyCallback, err = parseKnownHost(cfg.KnownHost)
		if err != nil {
			return nil, fmt.Errorf("parsing known_host: %w", err)
		}
	default:
		return nil, fmt.Errorf("host key verification required: set known_host, known_hosts_file, or insecure_skip_verify")
	}

	sshConfig := &gossh.ClientConfig{
		User:            cfg.Username,
		Auth:            []gossh.AuthMethod{gossh.PublicKeys(signer)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         time.Duration(cfg.TimeoutSecs) * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	conn, err := gossh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("SSH dial %s: %w", addr, err)
	}

	return &SSHClient{conn: conn}, nil
}

// Exec runs a command on the remote host and returns stdout, stderr, and exit code.
func (c *SSHClient) Exec(ctx context.Context, command string, cwd string) (string, string, int, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return "", "", -1, fmt.Errorf("client is closed")
	}
	c.mu.Unlock()

	session, err := c.conn.NewSession()
	if err != nil {
		return "", "", -1, fmt.Errorf("creating session: %w", err)
	}
	defer session.Close()

	var stdoutBuf, stderrBuf bytes.Buffer
	session.Stdout = &stdoutBuf
	session.Stderr = &stderrBuf

	cmd := command
	if cwd != "" {
		cmd = fmt.Sprintf("cd %s && %s", shellescape(cwd), command)
	}

	done := make(chan error, 1)
	go func() {
		done <- session.Run(cmd)
	}()

	select {
	case err := <-done:
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*gossh.ExitError); ok {
				exitCode = exitErr.ExitStatus()
			} else {
				return "", "", -1, err
			}
		}
		return stdoutBuf.String(), stderrBuf.String(), exitCode, nil
	case <-ctx.Done():
		session.Signal(gossh.SIGKILL)
		return "", "", -1, ctx.Err()
	}
}

// Close closes the SSH connection.
func (c *SSHClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return c.conn.Close()
}

// IsAlive checks if the SSH connection is still active.
func (c *SSHClient) IsAlive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return false
	}
	_, _, err := c.conn.SendRequest("keepalive@wardgate", true, nil)
	return err == nil
}

// parseKnownHost parses an inline known_hosts entry.
func parseKnownHost(entry string) (gossh.HostKeyCallback, error) {
	_, hosts, pubKey, _, _, err := gossh.ParseKnownHosts([]byte(entry))
	if err != nil {
		return nil, fmt.Errorf("parsing known host entry: %w", err)
	}

	return func(hostname string, remote net.Addr, key gossh.PublicKey) error {
		if pubKey.Type() != key.Type() {
			return fmt.Errorf("host key type mismatch: expected %s, got %s", pubKey.Type(), key.Type())
		}
		if !bytes.Equal(pubKey.Marshal(), key.Marshal()) {
			return fmt.Errorf("host key mismatch for %s", hostname)
		}
		host, _, _ := net.SplitHostPort(hostname)
		if host == "" {
			host = hostname
		}
		for _, h := range hosts {
			if h == host || h == hostname {
				return nil
			}
		}
		return fmt.Errorf("hostname %s not in known hosts list %v", hostname, hosts)
	}, nil
}

// shellescape wraps a string in single quotes for safe shell use.
func shellescape(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
