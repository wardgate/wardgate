package audit

import (
	"sort"
	"sync"
	"time"
)

// StoredEntry extends Entry with storage metadata.
type StoredEntry struct {
	Entry
	Timestamp   time.Time `json:"timestamp"`
	RequestBody string    `json:"request_body,omitempty"`
}

// QueryParams defines filters for querying the log store.
type QueryParams struct {
	Endpoint string
	AgentID  string
	Decision string
	Method   string
	Before   time.Time
	Limit    int
}

// Store is a thread-safe ring buffer for storing audit log entries.
type Store struct {
	mu       sync.RWMutex
	entries  []StoredEntry
	capacity int
	head     int // Next write position
	count    int // Number of entries (up to capacity)
}

// NewStore creates a new log store with the given capacity.
func NewStore(capacity int) *Store {
	if capacity <= 0 {
		capacity = 1000
	}
	return &Store{
		entries:  make([]StoredEntry, capacity),
		capacity: capacity,
	}
}

// Add adds an entry to the store.
func (s *Store) Add(entry StoredEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries[s.head] = entry
	s.head = (s.head + 1) % s.capacity
	if s.count < s.capacity {
		s.count++
	}
}

// Query returns entries matching the given parameters, newest first.
func (s *Store) Query(params QueryParams) []StoredEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if params.Limit <= 0 {
		params.Limit = 50
	}

	// Collect matching entries
	var results []StoredEntry
	for i := 0; i < s.count; i++ {
		// Calculate index (go backwards from head)
		idx := (s.head - 1 - i + s.capacity) % s.capacity
		entry := s.entries[idx]

		// Apply filters
		if params.Endpoint != "" && entry.Endpoint != params.Endpoint {
			continue
		}
		if params.AgentID != "" && entry.AgentID != params.AgentID {
			continue
		}
		if params.Decision != "" && entry.Decision != params.Decision {
			continue
		}
		if params.Method != "" && entry.Method != params.Method {
			continue
		}
		if !params.Before.IsZero() && !entry.Timestamp.Before(params.Before) {
			continue
		}

		results = append(results, entry)
		if len(results) >= params.Limit {
			break
		}
	}

	return results
}

// GetEndpoints returns a list of unique endpoint names in the store.
func (s *Store) GetEndpoints() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]bool)
	for i := 0; i < s.count; i++ {
		idx := (s.head - 1 - i + s.capacity) % s.capacity
		ep := s.entries[idx].Endpoint
		if ep != "" {
			seen[ep] = true
		}
	}

	result := make([]string, 0, len(seen))
	for ep := range seen {
		result = append(result, ep)
	}
	sort.Strings(result)
	return result
}

// GetAgents returns a list of unique agent IDs in the store.
func (s *Store) GetAgents() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	seen := make(map[string]bool)
	for i := 0; i < s.count; i++ {
		idx := (s.head - 1 - i + s.capacity) % s.capacity
		agent := s.entries[idx].AgentID
		if agent != "" {
			seen[agent] = true
		}
	}

	result := make([]string, 0, len(seen))
	for agent := range seen {
		result = append(result, agent)
	}
	sort.Strings(result)
	return result
}

// Count returns the number of entries in the store.
func (s *Store) Count() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.count
}
