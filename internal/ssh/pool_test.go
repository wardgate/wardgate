package ssh

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPool_GetClient(t *testing.T) {
	dialer := &mockDialer{}
	pool := NewPoolWithDialer(dialer, PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         5 * time.Minute,
	})

	client, err := pool.Get("test-endpoint", ConnectionConfig{
		Host:     "prod.example.com",
		Port:     22,
		Username: "deploy",
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client == nil {
		t.Fatal("expected client, got nil")
	}
}

func TestPool_ReusesClient(t *testing.T) {
	dialer := &mockDialer{}
	pool := NewPoolWithDialer(dialer, PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         5 * time.Minute,
	})

	cfg := ConnectionConfig{
		Host:     "prod.example.com",
		Port:     22,
		Username: "deploy",
	}

	client1, _ := pool.Get("test-endpoint", cfg)
	pool.Put("test-endpoint", client1)

	client2, _ := pool.Get("test-endpoint", cfg)

	if client1 != client2 {
		t.Error("expected client to be reused")
	}
	if dialer.dialCount != 1 {
		t.Errorf("expected 1 dial, got %d", dialer.dialCount)
	}
}

func TestPool_CreatesNewClientWhenNoneAvailable(t *testing.T) {
	dialer := &mockDialer{}
	pool := NewPoolWithDialer(dialer, PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         5 * time.Minute,
	})

	cfg := ConnectionConfig{
		Host:     "prod.example.com",
		Port:     22,
		Username: "deploy",
	}

	client1, _ := pool.Get("test-endpoint", cfg)
	client2, _ := pool.Get("test-endpoint", cfg)

	if client1 == client2 {
		t.Error("expected different clients")
	}
	if dialer.dialCount != 2 {
		t.Errorf("expected 2 dials, got %d", dialer.dialCount)
	}
	pool.Put("test-endpoint", client1)
	pool.Put("test-endpoint", client2)
}

func TestPool_RespectsMaxConnections(t *testing.T) {
	dialer := &mockDialer{}
	pool := NewPoolWithDialer(dialer, PoolConfig{
		MaxConnsPerEndpoint: 2,
		IdleTimeout:         5 * time.Minute,
	})

	cfg := ConnectionConfig{
		Host:     "prod.example.com",
		Port:     22,
		Username: "deploy",
	}

	clients := make([]Client, 2)
	for i := 0; i < 2; i++ {
		c, err := pool.Get("test-endpoint", cfg)
		if err != nil {
			t.Fatalf("unexpected error on client %d: %v", i, err)
		}
		clients[i] = c
	}

	// Third client should timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := pool.GetWithContext(ctx, "test-endpoint", cfg)
	if err == nil {
		t.Error("expected error when max connections reached")
	}
}

func TestPool_ConcurrentAccess(t *testing.T) {
	dialer := &mockDialer{}
	pool := NewPoolWithDialer(dialer, PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         5 * time.Minute,
	})

	cfg := ConnectionConfig{
		Host:     "prod.example.com",
		Port:     22,
		Username: "deploy",
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, err := pool.Get("test-endpoint", cfg)
			if err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
			pool.Put("test-endpoint", client)
		}()
	}
	wg.Wait()
}

func TestPool_ClosesIdleConnections(t *testing.T) {
	dialer := &mockDialer{}
	pool := NewPoolWithDialer(dialer, PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         50 * time.Millisecond,
	})

	cfg := ConnectionConfig{
		Host:     "prod.example.com",
		Port:     22,
		Username: "deploy",
	}

	client, _ := pool.Get("test-endpoint", cfg)
	pool.Put("test-endpoint", client)

	time.Sleep(100 * time.Millisecond)
	pool.CleanupIdle()

	client2, _ := pool.Get("test-endpoint", cfg)
	if client == client2 {
		t.Error("expected new client after idle cleanup")
	}
}

func TestPool_ClosesDeadConnections(t *testing.T) {
	dialer := &mockDialer{}
	pool := NewPoolWithDialer(dialer, PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         5 * time.Minute,
	})

	cfg := ConnectionConfig{
		Host:     "prod.example.com",
		Port:     22,
		Username: "deploy",
	}

	client, _ := pool.Get("test-endpoint", cfg)
	mc := client.(*mockPoolClient)
	mc.dead = true
	pool.Put("test-endpoint", client)

	client2, _ := pool.Get("test-endpoint", cfg)
	if client == client2 {
		t.Error("expected new client for dead one")
	}
}

func TestPool_Close(t *testing.T) {
	dialer := &mockDialer{}
	pool := NewPoolWithDialer(dialer, PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         5 * time.Minute,
	})

	cfg := ConnectionConfig{
		Host:     "prod.example.com",
		Port:     22,
		Username: "deploy",
	}

	client, _ := pool.Get("test-endpoint", cfg)
	pool.Put("test-endpoint", client)

	pool.Close()

	mc := client.(*mockPoolClient)
	if !mc.closed {
		t.Error("expected client to be closed")
	}
}

// Mock implementations for pool tests

type mockDialer struct {
	mu        sync.Mutex
	dialCount int
	failNext  bool
}

func (d *mockDialer) Dial(cfg ConnectionConfig) (Client, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.failNext {
		d.failNext = false
		return nil, ErrConnectionFailed
	}

	d.dialCount++
	return &mockPoolClient{id: d.dialCount}, nil
}

type mockPoolClient struct {
	id     int
	dead   bool
	closed bool
	mu     sync.Mutex
}

func (c *mockPoolClient) Exec(ctx context.Context, command string, cwd string) (string, string, int, error) {
	return "", "", 0, nil
}

func (c *mockPoolClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *mockPoolClient) IsAlive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.dead && !c.closed
}
