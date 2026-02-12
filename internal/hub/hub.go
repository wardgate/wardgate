package hub

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Message types (shared protocol with wardgate-exec).
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

// ConclaveConfig is the per-conclave configuration from wardgate's config.
type ConclaveConfig struct {
	Name   string
	KeyEnv string
}

// ConclaveInfo is the public status of a connected conclave.
type ConclaveInfo struct {
	Name      string    `json:"name"`
	Connected bool      `json:"connected"`
	Since     time.Time `json:"connected_since,omitempty"`
}

// conclaveConn tracks a single connected conclave.
type conclaveConn struct {
	name     string
	conn     *websocket.Conn
	writeMu  sync.Mutex
	since    time.Time
	lastPong time.Time
}

func (c *conclaveConn) sendJSON(v interface{}) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.conn.WriteJSON(v)
}

// Hub manages WebSocket connections from conclaves.
type Hub struct {
	version   string
	validKeys map[string]string // key -> conclave name
	upgrader  websocket.Upgrader

	mu    sync.RWMutex
	conns map[string]*conclaveConn // name -> conn

	pingInterval time.Duration
	pongTimeout  time.Duration
}

// NewHub creates a new conclave hub.
// conclaves maps conclave name -> key_env name.
func NewHub(version string, conclaves map[string]ConclaveConfig) *Hub {
	validKeys := make(map[string]string)
	for _, cc := range conclaves {
		key := os.Getenv(cc.KeyEnv)
		if key != "" {
			validKeys[key] = cc.Name
		} else {
			log.Printf("Warning: conclave %q key env %s is empty, conclave cannot connect", cc.Name, cc.KeyEnv)
		}
	}

	return &Hub{
		version:   version,
		validKeys: validKeys,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		conns:        make(map[string]*conclaveConn),
		pingInterval: 30 * time.Second,
		pongTimeout:  10 * time.Second,
	}
}

// ServeHTTP handles WebSocket upgrade requests from conclaves at /conclaves/ws.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Authenticate
	name, err := h.authenticate(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return
	}

	// Check for duplicate connection
	h.mu.RLock()
	_, exists := h.conns[name]
	h.mu.RUnlock()
	if exists {
		http.Error(w, fmt.Sprintf("conclave %q is already connected", name), http.StatusConflict)
		return
	}

	// Upgrade to WebSocket
	ws, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade for conclave %q: %v", name, err)
		return
	}

	cc := &conclaveConn{
		name:     name,
		conn:     ws,
		since:    time.Now(),
		lastPong: time.Now(),
	}

	// Register
	h.mu.Lock()
	h.conns[name] = cc
	h.mu.Unlock()

	log.Printf("Conclave %q connected", name)

	defer func() {
		h.mu.Lock()
		delete(h.conns, name)
		h.mu.Unlock()
		ws.Close()
		log.Printf("Conclave %q disconnected", name)
	}()

	// Send welcome
	if err := cc.sendJSON(map[string]string{
		"type":    MsgWelcome,
		"name":    name,
		"version": h.version,
	}); err != nil {
		log.Printf("Send welcome to %q: %v", name, err)
		return
	}

	// Start heartbeat
	done := make(chan struct{})
	defer close(done)
	go h.heartbeat(cc, done)

	// Read loop - messages from conclave are forwarded to pending requests
	h.readLoop(cc)
}

// authenticate validates the conclave's auth header and name header.
func (h *Hub) authenticate(r *http.Request) (string, error) {
	authHeader := r.Header.Get("Authorization")
	if !strings.HasPrefix(authHeader, "Bearer ") {
		return "", fmt.Errorf("missing or invalid authorization header")
	}

	token := strings.TrimPrefix(authHeader, "Bearer ")
	expectedName, ok := h.validKeys[token]
	if !ok {
		return "", fmt.Errorf("invalid conclave key")
	}

	// Verify the claimed name matches the key
	claimedName := r.Header.Get("X-Conclave-Name")
	if claimedName != expectedName {
		return "", fmt.Errorf("conclave name mismatch: key is for %q, claimed %q", expectedName, claimedName)
	}

	return expectedName, nil
}

// heartbeat sends pings and checks for pong responses.
func (h *Hub) heartbeat(cc *conclaveConn, done <-chan struct{}) {
	ticker := time.NewTicker(h.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if err := cc.sendJSON(map[string]string{"type": MsgPing}); err != nil {
				log.Printf("Ping %q: %v", cc.name, err)
				cc.conn.Close()
				return
			}

			// Check if last pong is too old
			cc.writeMu.Lock()
			lastPong := cc.lastPong
			cc.writeMu.Unlock()

			if time.Since(lastPong) > h.pingInterval+h.pongTimeout {
				log.Printf("Conclave %q pong timeout", cc.name)
				cc.conn.Close()
				return
			}
		}
	}
}

// readLoop reads messages from a conclave connection.
// stdout/stderr/exit/error messages are routed to pending request waiters.
func (h *Hub) readLoop(cc *conclaveConn) {
	for {
		_, raw, err := cc.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("Read from %q: %v", cc.name, err)
			}
			return
		}

		var msg struct {
			Type string `json:"type"`
			ID   string `json:"id,omitempty"`
		}
		if err := json.Unmarshal(raw, &msg); err != nil {
			log.Printf("Invalid message from %q: %v", cc.name, err)
			continue
		}

		switch msg.Type {
		case MsgPong:
			cc.writeMu.Lock()
			cc.lastPong = time.Now()
			cc.writeMu.Unlock()

		case MsgStdout, MsgStderr, MsgExit, MsgError:
			h.mu.RLock()
			waiter, ok := h.getWaiter(cc.name, msg.ID)
			h.mu.RUnlock()
			if ok {
				waiter <- raw
			}

		default:
			log.Printf("Unexpected message type %q from %q", msg.Type, cc.name)
		}
	}
}

// --- Request routing (exec forwarding) ---

// pendingReq tracks a pending exec request waiting for results.
type pendingReq struct {
	ch chan []byte
}

var (
	pendingMu sync.RWMutex
	pending   = make(map[string]*pendingReq) // "conclave:reqID" -> waiter
)

func pendingKey(conclave, reqID string) string {
	return conclave + ":" + reqID
}

func (h *Hub) getWaiter(conclave, reqID string) (chan []byte, bool) {
	pendingMu.RLock()
	defer pendingMu.RUnlock()
	p, ok := pending[pendingKey(conclave, reqID)]
	if !ok {
		return nil, false
	}
	return p.ch, true
}

// SendExec sends an exec request to a conclave and returns a channel that
// receives raw JSON messages (stdout, stderr, exit, error) for that request.
// The caller must call CleanupExec when done.
func (h *Hub) SendExec(conclave, reqID, command, args, cwd string) (<-chan []byte, error) {
	h.mu.RLock()
	cc, ok := h.conns[conclave]
	h.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("conclave %q is not connected", conclave)
	}

	// Register waiter before sending to avoid races
	ch := make(chan []byte, 64)
	pendingMu.Lock()
	pending[pendingKey(conclave, reqID)] = &pendingReq{ch: ch}
	pendingMu.Unlock()

	err := cc.sendJSON(map[string]interface{}{
		"type":    MsgExec,
		"id":      reqID,
		"command": command,
		"args":    args,
		"cwd":     cwd,
	})
	if err != nil {
		h.CleanupExec(conclave, reqID)
		return nil, fmt.Errorf("send exec to %q: %w", conclave, err)
	}

	return ch, nil
}

// SendKill sends a kill request for a running command on a conclave.
func (h *Hub) SendKill(conclave, reqID string) error {
	h.mu.RLock()
	cc, ok := h.conns[conclave]
	h.mu.RUnlock()
	if !ok {
		return fmt.Errorf("conclave %q is not connected", conclave)
	}

	return cc.sendJSON(map[string]string{
		"type": MsgKill,
		"id":   reqID,
	})
}

// CleanupExec removes a pending request waiter.
func (h *Hub) CleanupExec(conclave, reqID string) {
	pendingMu.Lock()
	delete(pending, pendingKey(conclave, reqID))
	pendingMu.Unlock()
}

// Conclaves returns the status of all configured conclaves.
func (h *Hub) Conclaves() []ConclaveInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var infos []ConclaveInfo
	// Include all known names (from validKeys)
	seen := make(map[string]bool)
	for _, name := range h.validKeys {
		if seen[name] {
			continue
		}
		seen[name] = true

		info := ConclaveInfo{Name: name}
		if cc, ok := h.conns[name]; ok {
			info.Connected = true
			info.Since = cc.since
		}
		infos = append(infos, info)
	}
	return infos
}

// IsConnected returns whether a conclave is currently connected.
func (h *Hub) IsConnected(name string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	_, ok := h.conns[name]
	return ok
}
