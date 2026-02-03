package imap

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrConnectionFailed  = errors.New("failed to connect to IMAP server")
	ErrMaxConnsReached   = errors.New("maximum connections reached")
	ErrConnectionTimeout = errors.New("connection timeout")
)

// ConnectionConfig holds IMAP connection parameters.
type ConnectionConfig struct {
	Host               string
	Port               int
	Username           string
	Password           string
	TLS                bool
	InsecureSkipVerify bool // Skip TLS cert verification (for self-signed certs like ProtonBridge)
}

// Folder represents an IMAP folder.
type Folder struct {
	Name      string `json:"name"`
	Delimiter string `json:"delimiter"`
}

// FolderStatus represents the status of a selected folder.
type FolderStatus struct {
	Name     string `json:"name"`
	Messages uint32 `json:"messages"`
	Unseen   uint32 `json:"unseen"`
}

// Message represents an email message.
type Message struct {
	UID     uint32    `json:"uid"`
	Subject string    `json:"subject"`
	From    string    `json:"from"`
	To      []string  `json:"to,omitempty"`
	Date    time.Time `json:"date"`
	Body    string    `json:"body,omitempty"`
	Seen    bool      `json:"seen"`
}

// FetchOptions defines options for fetching messages.
type FetchOptions struct {
	Folder string
	Limit  int
	Since  *time.Time
	Before *time.Time
}

// Connection represents an IMAP connection.
type Connection interface {
	IsAlive() bool
	Close() error
	ListFolders(ctx context.Context) ([]Folder, error)
	SelectFolder(ctx context.Context, folder string) (*FolderStatus, error)
	FetchMessages(ctx context.Context, opts FetchOptions) ([]Message, error)
	GetMessage(ctx context.Context, uid uint32) (*Message, error)
	MarkRead(ctx context.Context, uid uint32) error
	MoveMessage(ctx context.Context, uid uint32, destFolder string) error
}

// Dialer creates IMAP connections.
type Dialer interface {
	Dial(ctx context.Context, cfg ConnectionConfig) (Connection, error)
}

// PoolConfig configures the connection pool.
type PoolConfig struct {
	MaxConnsPerEndpoint int
	IdleTimeout         time.Duration
}

// pooledConn wraps a connection with metadata.
type pooledConn struct {
	conn       Connection
	lastUsed   time.Time
	inUse      bool
}

// endpointPool manages connections for a single endpoint.
type endpointPool struct {
	mu       sync.Mutex
	conns    []*pooledConn
	config   ConnectionConfig
	maxConns int
	sem      chan struct{}
}

// Pool manages IMAP connections across multiple endpoints.
type Pool struct {
	dialer      Dialer
	config      PoolConfig
	mu          sync.RWMutex
	pools       map[string]*endpointPool
}

// NewPool creates a new connection pool.
func NewPool(dialer Dialer, cfg PoolConfig) *Pool {
	if cfg.MaxConnsPerEndpoint <= 0 {
		cfg.MaxConnsPerEndpoint = 5
	}
	if cfg.IdleTimeout <= 0 {
		cfg.IdleTimeout = 5 * time.Minute
	}
	return &Pool{
		dialer: dialer,
		config: cfg,
		pools:  make(map[string]*endpointPool),
	}
}

// Get retrieves a connection from the pool or creates a new one.
func (p *Pool) Get(ctx context.Context, endpoint string, cfg ConnectionConfig) (Connection, error) {
	ep := p.getOrCreateEndpointPool(endpoint, cfg)

	// Try to get semaphore slot
	select {
	case ep.sem <- struct{}{}:
		// Got a slot
	case <-ctx.Done():
		return nil, ErrConnectionTimeout
	}

	ep.mu.Lock()
	// Look for available connection
	for _, pc := range ep.conns {
		if !pc.inUse && pc.conn != nil && pc.conn.IsAlive() {
			pc.inUse = true
			pc.lastUsed = time.Now()
			ep.mu.Unlock()
			return pc.conn, nil
		}
	}
	ep.mu.Unlock()

	// Create new connection
	conn, err := p.dialer.Dial(ctx, cfg)
	if err != nil {
		<-ep.sem // Release slot
		return nil, err
	}

	ep.mu.Lock()
	ep.conns = append(ep.conns, &pooledConn{
		conn:     conn,
		lastUsed: time.Now(),
		inUse:    true,
	})
	ep.mu.Unlock()

	return conn, nil
}

// Put returns a connection to the pool.
func (p *Pool) Put(endpoint string, conn Connection) {
	p.mu.RLock()
	ep, ok := p.pools[endpoint]
	p.mu.RUnlock()

	if !ok {
		conn.Close()
		return
	}

	ep.mu.Lock()
	for _, pc := range ep.conns {
		if pc.conn == conn {
			if !conn.IsAlive() {
				// Remove dead connection
				pc.conn.Close()
				pc.inUse = false
				pc.conn = nil
			} else {
				pc.inUse = false
				pc.lastUsed = time.Now()
			}
			break
		}
	}
	ep.mu.Unlock()

	// Release semaphore
	select {
	case <-ep.sem:
	default:
	}
}

// CleanupIdle removes idle connections that have exceeded the timeout.
func (p *Pool) CleanupIdle() {
	p.mu.RLock()
	pools := make(map[string]*endpointPool, len(p.pools))
	for k, v := range p.pools {
		pools[k] = v
	}
	p.mu.RUnlock()

	now := time.Now()
	for _, ep := range pools {
		ep.mu.Lock()
		newConns := make([]*pooledConn, 0, len(ep.conns))
		for _, pc := range ep.conns {
			if pc.conn == nil {
				continue
			}
			if !pc.inUse && now.Sub(pc.lastUsed) > p.config.IdleTimeout {
				pc.conn.Close()
				// Release semaphore for closed connection
				select {
				case <-ep.sem:
				default:
				}
				continue
			}
			newConns = append(newConns, pc)
		}
		ep.conns = newConns
		ep.mu.Unlock()
	}
}

// Close closes all connections in the pool.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, ep := range p.pools {
		ep.mu.Lock()
		for _, pc := range ep.conns {
			if pc.conn != nil {
				pc.conn.Close()
			}
		}
		ep.conns = nil
		ep.mu.Unlock()
	}
	p.pools = make(map[string]*endpointPool)
	return nil
}

func (p *Pool) getOrCreateEndpointPool(endpoint string, cfg ConnectionConfig) *endpointPool {
	p.mu.RLock()
	ep, ok := p.pools[endpoint]
	p.mu.RUnlock()

	if ok {
		return ep
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if ep, ok = p.pools[endpoint]; ok {
		return ep
	}

	ep = &endpointPool{
		config:   cfg,
		maxConns: p.config.MaxConnsPerEndpoint,
		sem:      make(chan struct{}, p.config.MaxConnsPerEndpoint),
	}
	p.pools[endpoint] = ep
	return ep
}
