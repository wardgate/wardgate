package conclave

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Message types for the WebSocket protocol.
const (
	MsgWelcome = "welcome"
	MsgPing    = "ping"
	MsgPong    = "pong"
	MsgExec    = "exec"
	MsgKill    = "kill"
	MsgStdout  = "stdout"
	MsgStderr  = "stderr"
	MsgExit    = "exit"
	MsgError   = "error"
)

// ServerMessage is a message from wardgate to wardgate-exec.
type ServerMessage struct {
	Type    string `json:"type"`
	ID      string `json:"id,omitempty"`
	Name    string `json:"name,omitempty"`    // welcome
	Version string `json:"version,omitempty"` // welcome
	Command string `json:"command,omitempty"` // exec
	Args    string `json:"args,omitempty"`    // exec
	Cwd     string `json:"cwd,omitempty"`     // exec
}

// ClientMessage is a message from wardgate-exec to wardgate.
type ClientMessage struct {
	Type       string `json:"type"`
	ID         string `json:"id,omitempty"`
	Data       string `json:"data,omitempty"`        // stdout, stderr
	Code       int    `json:"code,omitempty"`         // exit
	DurationMs int64  `json:"duration_ms,omitempty"`  // exit
	Message    string `json:"message,omitempty"`       // error
}

// Client is the wardgate-exec WebSocket client that connects to wardgate
// and processes command execution requests.
type Client struct {
	cfg      *Config
	executor *Executor

	// Track running commands for kill support
	mu       sync.Mutex
	cancels  map[string]context.CancelFunc
}

// NewClient creates a new wardgate-exec client.
func NewClient(cfg *Config, executor *Executor) *Client {
	return &Client{
		cfg:      cfg,
		executor: executor,
		cancels:  make(map[string]context.CancelFunc),
	}
}

// Run connects to wardgate and processes commands until ctx is cancelled.
// It reconnects automatically on disconnect with exponential backoff.
func (c *Client) Run(ctx context.Context) error {
	backoff := time.Second
	const maxBackoff = 60 * time.Second

	for {
		err := c.connect(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		log.Printf("Disconnected: %v", err)

		// Fail any running commands
		c.failRunningCommands()

		// Exponential backoff with jitter
		jitter := time.Duration(float64(backoff) * (0.8 + 0.4*rand.Float64()))
		log.Printf("Reconnecting in %s...", jitter.Round(time.Millisecond))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(jitter):
		}

		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// connect establishes a single WebSocket connection and processes messages
// until disconnect or error.
func (c *Client) connect(ctx context.Context) error {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+c.cfg.Key)
	header.Set("X-Conclave-Name", c.cfg.Name)

	log.Printf("Connecting to %s as %q...", c.cfg.Server, c.cfg.Name)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, c.cfg.Server, header)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	// Read welcome message
	var welcome ServerMessage
	if err := conn.ReadJSON(&welcome); err != nil {
		return fmt.Errorf("read welcome: %w", err)
	}
	if welcome.Type != MsgWelcome {
		return fmt.Errorf("expected welcome, got %q", welcome.Type)
	}
	log.Printf("Connected as %q (server version: %s)", welcome.Name, welcome.Version)

	// Reset backoff on successful connection (caller handles this implicitly
	// since we return nil only on clean shutdown).

	// Create a write mutex for the connection
	var writeMu sync.Mutex
	send := func(msg ClientMessage) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(msg)
	}

	// Read messages
	for {
		select {
		case <-ctx.Done():
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"))
			return nil
		default:
		}

		var msg ServerMessage
		if err := conn.ReadJSON(&msg); err != nil {
			return fmt.Errorf("read: %w", err)
		}

		switch msg.Type {
		case MsgPing:
			if err := send(ClientMessage{Type: MsgPong}); err != nil {
				return fmt.Errorf("send pong: %w", err)
			}

		case MsgExec:
			go c.handleExec(ctx, msg, send)

		case MsgKill:
			c.handleKill(msg.ID)

		default:
			log.Printf("Unknown message type: %q", msg.Type)
		}
	}
}

// handleExec executes a command and streams output back over the WebSocket.
func (c *Client) handleExec(ctx context.Context, msg ServerMessage, send func(ClientMessage) error) {
	execCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Register cancel func for kill support
	c.mu.Lock()
	c.cancels[msg.ID] = cancel
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.cancels, msg.ID)
		c.mu.Unlock()
	}()

	req := ExecRequest{
		ID:      msg.ID,
		Command: msg.Command,
		Args:    msg.Args,
		Cwd:     msg.Cwd,
	}

	result := c.executor.Execute(execCtx, req, func(chunk OutputChunk) {
		if err := send(ClientMessage{
			Type: chunk.Stream,
			ID:   chunk.ID,
			Data: chunk.Data,
		}); err != nil {
			log.Printf("Send %s for %s: %v", chunk.Stream, chunk.ID, err)
		}
	})

	// Send error message if command failed to start
	if result.Error != "" {
		send(ClientMessage{
			Type:    MsgError,
			ID:      result.ID,
			Message: result.Error,
			Code:    result.Code,
		})
		return
	}

	// Send exit
	send(ClientMessage{
		Type:       MsgExit,
		ID:         result.ID,
		Code:       result.Code,
		DurationMs: result.DurationMs,
	})
}

// handleKill cancels a running command.
func (c *Client) handleKill(id string) {
	c.mu.Lock()
	cancel, ok := c.cancels[id]
	c.mu.Unlock()

	if ok {
		log.Printf("Killing command %s", id)
		cancel()
	}
}

// failRunningCommands cancels all in-flight commands (called on disconnect).
func (c *Client) failRunningCommands() {
	c.mu.Lock()
	defer c.mu.Unlock()

	for id, cancel := range c.cancels {
		log.Printf("Failing command %s due to disconnect", id)
		cancel()
		delete(c.cancels, id)
	}
}
