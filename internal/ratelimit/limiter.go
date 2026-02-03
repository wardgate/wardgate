package ratelimit

import (
	"sync"
	"time"
)

// Limiter implements a sliding window rate limiter.
type Limiter struct {
	mu       sync.Mutex
	requests []time.Time
	max      int
	window   time.Duration
}

// New creates a rate limiter with max requests per window duration.
func New(max int, window time.Duration) *Limiter {
	return &Limiter{
		requests: make([]time.Time, 0, max),
		max:      max,
		window:   window,
	}
}

// Allow checks if a request is allowed under the rate limit.
// Returns true if allowed, false if rate limited.
func (l *Limiter) Allow() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)

	// Remove expired entries
	valid := l.requests[:0]
	for _, t := range l.requests {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	l.requests = valid

	// Check limit
	if len(l.requests) >= l.max {
		return false
	}

	// Record this request
	l.requests = append(l.requests, now)
	return true
}

// Count returns the current count of requests in the window.
func (l *Limiter) Count() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-l.window)

	count := 0
	for _, t := range l.requests {
		if t.After(cutoff) {
			count++
		}
	}
	return count
}

// Reset clears all recorded requests.
func (l *Limiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = l.requests[:0]
}

// Registry manages rate limiters per key.
type Registry struct {
	mu       sync.RWMutex
	limiters map[string]*Limiter
	max      int
	window   time.Duration
}

// NewRegistry creates a registry with default rate limit settings.
func NewRegistry(max int, window time.Duration) *Registry {
	return &Registry{
		limiters: make(map[string]*Limiter),
		max:      max,
		window:   window,
	}
}

// Get returns the limiter for the given key, creating one if needed.
func (r *Registry) Get(key string) *Limiter {
	r.mu.RLock()
	lim, ok := r.limiters[key]
	r.mu.RUnlock()
	if ok {
		return lim
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	// Double-check after acquiring write lock
	if lim, ok = r.limiters[key]; ok {
		return lim
	}

	lim = New(r.max, r.window)
	r.limiters[key] = lim
	return lim
}

// Allow checks if a request for the given key is allowed.
func (r *Registry) Allow(key string) bool {
	return r.Get(key).Allow()
}
