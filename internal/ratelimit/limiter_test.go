package ratelimit

import (
	"testing"
	"time"
)

func TestLimiter_AllowWithinLimit(t *testing.T) {
	lim := New(5, time.Minute)

	for i := 0; i < 5; i++ {
		if !lim.Allow() {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
}

func TestLimiter_DenyOverLimit(t *testing.T) {
	lim := New(3, time.Minute)

	// Use up the limit
	for i := 0; i < 3; i++ {
		lim.Allow()
	}

	// 4th request should be denied
	if lim.Allow() {
		t.Error("request over limit should be denied")
	}
}

func TestLimiter_SlidingWindow(t *testing.T) {
	lim := New(2, 50*time.Millisecond)

	// Use up limit
	lim.Allow()
	lim.Allow()

	// Should be denied
	if lim.Allow() {
		t.Error("should be denied at limit")
	}

	// Wait for window to slide
	time.Sleep(60 * time.Millisecond)

	// Should be allowed again
	if !lim.Allow() {
		t.Error("should be allowed after window expires")
	}
}

func TestLimiter_Count(t *testing.T) {
	lim := New(10, time.Minute)

	lim.Allow()
	lim.Allow()
	lim.Allow()

	if lim.Count() != 3 {
		t.Errorf("expected count 3, got %d", lim.Count())
	}
}

func TestLimiter_Reset(t *testing.T) {
	lim := New(2, time.Minute)

	lim.Allow()
	lim.Allow()

	if lim.Allow() {
		t.Error("should be denied before reset")
	}

	lim.Reset()

	if !lim.Allow() {
		t.Error("should be allowed after reset")
	}
}

func TestRegistry_Get(t *testing.T) {
	reg := NewRegistry(10, time.Minute)

	lim1 := reg.Get("agent-1")
	lim2 := reg.Get("agent-1")
	lim3 := reg.Get("agent-2")

	if lim1 != lim2 {
		t.Error("same key should return same limiter")
	}
	if lim1 == lim3 {
		t.Error("different keys should return different limiters")
	}
}

func TestRegistry_Allow(t *testing.T) {
	reg := NewRegistry(2, time.Minute)

	if !reg.Allow("agent-1") {
		t.Error("first request should be allowed")
	}
	if !reg.Allow("agent-1") {
		t.Error("second request should be allowed")
	}
	if reg.Allow("agent-1") {
		t.Error("third request should be denied")
	}

	// Different key should have its own limit
	if !reg.Allow("agent-2") {
		t.Error("different agent should be allowed")
	}
}
