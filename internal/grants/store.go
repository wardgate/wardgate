package grants

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Grant is a dynamic policy rule that can be permanent or time-limited.
type Grant struct {
	ID        string     `json:"id"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"` // nil = permanent
	AgentID   string     `json:"agent_id"`             // "*" = any agent
	Scope     string     `json:"scope"`                // "endpoint:name" or "conclave:name"
	Match     GrantMatch `json:"match"`
	Action    string     `json:"action"`
	Reason    string     `json:"reason,omitempty"`
}

// GrantMatch defines what the grant matches on.
type GrantMatch struct {
	// For HTTP endpoints
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
	// For conclaves
	Command     string `json:"command,omitempty"`
	ArgsPattern string `json:"args_pattern,omitempty"`
	CwdPattern  string `json:"cwd_pattern,omitempty"`
}

// Store manages grants with thread-safe access and optional file persistence.
type Store struct {
	mu     sync.RWMutex
	grants []*Grant
	path   string // file path for persistence (empty = no persistence)
}

// NewStore creates a new grant store. If path is non-empty, grants are persisted to that file.
func NewStore(path string) *Store {
	return &Store{path: path}
}

// LoadStore loads grants from a file. Returns a new empty store if the file doesn't exist.
func LoadStore(path string) (*Store, error) {
	s := &Store{path: path}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read grants file: %w", err)
	}
	if err := json.Unmarshal(data, &s.grants); err != nil {
		return nil, fmt.Errorf("parse grants file: %w", err)
	}
	s.Prune()
	return s, nil
}

// Add adds a grant to the store. It auto-generates an ID and sets CreatedAt.
func (s *Store) Add(g Grant) *Grant {
	s.mu.Lock()
	defer s.mu.Unlock()

	if g.ID == "" {
		g.ID = generateGrantID()
	}
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now()
	}

	s.grants = append(s.grants, &g)
	s.saveLocked()
	return &g
}

// Revoke removes a grant by ID.
func (s *Store) Revoke(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, g := range s.grants {
		if g.ID == id {
			s.grants = append(s.grants[:i], s.grants[i+1:]...)
			s.saveLocked()
			return nil
		}
	}
	return fmt.Errorf("grant %q not found", id)
}

// List returns all non-expired grants.
func (s *Store) List() []*Grant {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	var result []*Grant
	for _, g := range s.grants {
		if g.ExpiresAt != nil && now.After(*g.ExpiresAt) {
			continue
		}
		result = append(result, g)
	}
	return result
}

// CheckExec checks if there's a matching grant for an exec request.
func (s *Store) CheckExec(agentID, scope, command, args, cwd string) *Grant {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	for _, g := range s.grants {
		if g.ExpiresAt != nil && now.After(*g.ExpiresAt) {
			continue
		}
		if !matchAgent(g.AgentID, agentID) {
			continue
		}
		if g.Scope != scope {
			continue
		}
		if g.Match.Command != "" && g.Match.Command != command {
			continue
		}
		// Match found
		return g
	}
	return nil
}

// CheckHTTP checks if there's a matching grant for an HTTP request.
func (s *Store) CheckHTTP(agentID, scope, method, path string) *Grant {
	s.mu.RLock()
	defer s.mu.RUnlock()

	now := time.Now()
	for _, g := range s.grants {
		if g.ExpiresAt != nil && now.After(*g.ExpiresAt) {
			continue
		}
		if !matchAgent(g.AgentID, agentID) {
			continue
		}
		if g.Scope != scope {
			continue
		}
		if g.Match.Method != "" && g.Match.Method != method {
			continue
		}
		if g.Match.Path != "" && !matchPath(g.Match.Path, path) {
			continue
		}
		return g
	}
	return nil
}

// Prune removes expired grants from the store.
func (s *Store) Prune() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var kept []*Grant
	for _, g := range s.grants {
		if g.ExpiresAt != nil && now.After(*g.ExpiresAt) {
			continue
		}
		kept = append(kept, g)
	}
	s.grants = kept
	s.saveLocked()
}

// Get returns a grant by ID.
func (s *Store) Get(id string) (*Grant, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, g := range s.grants {
		if g.ID == id {
			return g, true
		}
	}
	return nil, false
}

func (s *Store) saveLocked() {
	if s.path == "" {
		return
	}

	data, err := json.MarshalIndent(s.grants, "", "  ")
	if err != nil {
		return
	}

	// Atomic write: write to temp file, then rename
	dir := filepath.Dir(s.path)
	tmp, err := os.CreateTemp(dir, ".grants-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpName)
		return
	}
	tmp.Close()
	os.Rename(tmpName, s.path)
}

func matchAgent(grantAgent, requestAgent string) bool {
	if grantAgent == "*" {
		return true
	}
	return grantAgent == requestAgent
}

func matchPath(pattern, path string) bool {
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(path, prefix)
	}
	return pattern == path
}

func generateGrantID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "grant_" + hex.EncodeToString(b)
}
