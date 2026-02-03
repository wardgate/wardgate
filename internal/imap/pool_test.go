package imap

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestPool_GetConnection(t *testing.T) {
	dialer := &mockDialer{conns: make(map[string]*mockConn)}
	pool := NewPool(dialer, PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         5 * time.Minute,
	})

	conn, err := pool.Get(context.Background(), "test-endpoint", ConnectionConfig{
		Host:     "imap.example.com",
		Port:     993,
		Username: "user",
		Password: "pass",
		TLS:      true,
	})

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if conn == nil {
		t.Fatal("expected connection, got nil")
	}
}

func TestPool_ReusesConnection(t *testing.T) {
	dialer := &mockDialer{conns: make(map[string]*mockConn)}
	pool := NewPool(dialer, PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         5 * time.Minute,
	})

	cfg := ConnectionConfig{
		Host:     "imap.example.com",
		Port:     993,
		Username: "user",
		Password: "pass",
		TLS:      true,
	}

	conn1, _ := pool.Get(context.Background(), "test-endpoint", cfg)
	pool.Put("test-endpoint", conn1)

	conn2, _ := pool.Get(context.Background(), "test-endpoint", cfg)

	if conn1 != conn2 {
		t.Error("expected connection to be reused")
	}
	if dialer.dialCount != 1 {
		t.Errorf("expected 1 dial, got %d", dialer.dialCount)
	}
}

func TestPool_CreatesNewConnectionWhenNoneAvailable(t *testing.T) {
	dialer := &mockDialer{conns: make(map[string]*mockConn)}
	pool := NewPool(dialer, PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         5 * time.Minute,
	})

	cfg := ConnectionConfig{
		Host:     "imap.example.com",
		Port:     993,
		Username: "user",
		Password: "pass",
		TLS:      true,
	}

	conn1, _ := pool.Get(context.Background(), "test-endpoint", cfg)
	// Don't return to pool
	conn2, _ := pool.Get(context.Background(), "test-endpoint", cfg)

	if conn1 == conn2 {
		t.Error("expected different connections")
	}
	if dialer.dialCount != 2 {
		t.Errorf("expected 2 dials, got %d", dialer.dialCount)
	}
	pool.Put("test-endpoint", conn1)
	pool.Put("test-endpoint", conn2)
}

func TestPool_RespectsMaxConnections(t *testing.T) {
	dialer := &mockDialer{conns: make(map[string]*mockConn)}
	pool := NewPool(dialer, PoolConfig{
		MaxConnsPerEndpoint: 2,
		IdleTimeout:         5 * time.Minute,
	})

	cfg := ConnectionConfig{
		Host:     "imap.example.com",
		Port:     993,
		Username: "user",
		Password: "pass",
		TLS:      true,
	}

	conns := make([]Connection, 2)
	for i := 0; i < 2; i++ {
		conn, err := pool.Get(context.Background(), "test-endpoint", cfg)
		if err != nil {
			t.Fatalf("unexpected error on conn %d: %v", i, err)
		}
		conns[i] = conn
	}

	// Third connection should block or fail
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := pool.Get(ctx, "test-endpoint", cfg)
	if err == nil {
		t.Error("expected error when max connections reached")
	}
}

func TestPool_ConcurrentAccess(t *testing.T) {
	dialer := &mockDialer{conns: make(map[string]*mockConn)}
	pool := NewPool(dialer, PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         5 * time.Minute,
	})

	cfg := ConnectionConfig{
		Host:     "imap.example.com",
		Port:     993,
		Username: "user",
		Password: "pass",
		TLS:      true,
	}

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := pool.Get(context.Background(), "test-endpoint", cfg)
			if err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
			pool.Put("test-endpoint", conn)
		}()
	}
	wg.Wait()
}

func TestPool_ClosesIdleConnections(t *testing.T) {
	dialer := &mockDialer{conns: make(map[string]*mockConn)}
	pool := NewPool(dialer, PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         50 * time.Millisecond,
	})

	cfg := ConnectionConfig{
		Host:     "imap.example.com",
		Port:     993,
		Username: "user",
		Password: "pass",
		TLS:      true,
	}

	conn, _ := pool.Get(context.Background(), "test-endpoint", cfg)
	pool.Put("test-endpoint", conn)

	// Wait for idle timeout
	time.Sleep(100 * time.Millisecond)
	pool.CleanupIdle()

	// Next get should create new connection
	conn2, _ := pool.Get(context.Background(), "test-endpoint", cfg)
	if conn == conn2 {
		t.Error("expected new connection after idle cleanup")
	}
}

func TestPool_ClosesDeadConnections(t *testing.T) {
	dialer := &mockDialer{conns: make(map[string]*mockConn)}
	pool := NewPool(dialer, PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         5 * time.Minute,
	})

	cfg := ConnectionConfig{
		Host:     "imap.example.com",
		Port:     993,
		Username: "user",
		Password: "pass",
		TLS:      true,
	}

	conn, _ := pool.Get(context.Background(), "test-endpoint", cfg)
	mockConn := conn.(*mockConn)
	mockConn.dead = true
	pool.Put("test-endpoint", conn)

	// Should create new connection since old one is dead
	conn2, _ := pool.Get(context.Background(), "test-endpoint", cfg)
	if conn == conn2 {
		t.Error("expected new connection for dead one")
	}
}

// Mock implementations for testing

type mockDialer struct {
	mu        sync.Mutex
	conns     map[string]*mockConn
	dialCount int
	failNext  bool
}

func (d *mockDialer) Dial(ctx context.Context, cfg ConnectionConfig) (Connection, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.failNext {
		d.failNext = false
		return nil, ErrConnectionFailed
	}

	d.dialCount++
	conn := &mockConn{
		id:       d.dialCount,
		host:     cfg.Host,
		username: cfg.Username,
	}
	return conn, nil
}

type mockConn struct {
	id         int
	host       string
	username   string
	dead       bool
	closed     bool
	mu         sync.Mutex
	messages   []Message
	lastOpts   FetchOptions
	markedRead uint32
	movedUID   uint32
	movedTo    string
}

func (c *mockConn) IsAlive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.dead && !c.closed
}

func (c *mockConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

func (c *mockConn) ListFolders(ctx context.Context) ([]Folder, error) {
	return []Folder{
		{Name: "INBOX", Delimiter: "/"},
		{Name: "Sent", Delimiter: "/"},
		{Name: "Drafts", Delimiter: "/"},
	}, nil
}

func (c *mockConn) SelectFolder(ctx context.Context, folder string) (*FolderStatus, error) {
	return &FolderStatus{
		Name:     folder,
		Messages: 42,
		Unseen:   5,
	}, nil
}

func (c *mockConn) FetchMessages(ctx context.Context, opts FetchOptions) ([]Message, error) {
	c.mu.Lock()
	c.lastOpts = opts
	c.mu.Unlock()

	if c.messages != nil {
		return c.messages, nil
	}
	return []Message{
		{
			UID:     1,
			Subject: "Test email",
			From:    "sender@example.com",
			Date:    time.Now(),
		},
	}, nil
}

func (c *mockConn) GetMessage(ctx context.Context, uid uint32) (*Message, error) {
	return &Message{
		UID:     uid,
		Subject: "Test email",
		From:    "sender@example.com",
		Date:    time.Now(),
		Body:    "This is the message body",
	}, nil
}

func (c *mockConn) MarkRead(ctx context.Context, uid uint32) error {
	c.mu.Lock()
	c.markedRead = uid
	c.mu.Unlock()
	return nil
}

func (c *mockConn) MoveMessage(ctx context.Context, uid uint32, destFolder string) error {
	c.mu.Lock()
	c.movedUID = uid
	c.movedTo = destFolder
	c.mu.Unlock()
	return nil
}
