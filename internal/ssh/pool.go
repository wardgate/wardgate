package ssh

import (
	"context"
	"sync"
	"time"
)

// Dialer creates SSH client connections.
type Dialer interface {
	Dial(cfg ConnectionConfig) (Client, error)
}

// PoolConfig configures the connection pool.
type PoolConfig struct {
	MaxConnsPerEndpoint int
	IdleTimeout         time.Duration
}

// pooledClient wraps a client with metadata.
type pooledClient struct {
	client   Client
	lastUsed time.Time
	inUse    bool
}

// endpointPool manages connections for a single endpoint.
type endpointPool struct {
	mu       sync.Mutex
	clients  []*pooledClient
	maxConns int
	sem      chan struct{}
}

// Pool manages SSH connections across multiple endpoints.
type Pool struct {
	dialer Dialer
	config PoolConfig
	mu     sync.RWMutex
	pools  map[string]*endpointPool
}

// NewPool creates a new SSH connection pool with no dialer (for production use with real SSH).
func NewPool() *Pool {
	return &Pool{
		config: PoolConfig{MaxConnsPerEndpoint: 5, IdleTimeout: 5 * time.Minute},
		pools:  make(map[string]*endpointPool),
	}
}

// NewPoolWithDialer creates a new SSH connection pool with a custom dialer.
func NewPoolWithDialer(dialer Dialer, cfg PoolConfig) *Pool {
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

// Get retrieves a client from the pool or creates a new one.
func (p *Pool) Get(endpoint string, cfg ConnectionConfig) (Client, error) {
	return p.GetWithContext(context.Background(), endpoint, cfg)
}

// GetWithContext retrieves a client with context support for cancellation.
func (p *Pool) GetWithContext(ctx context.Context, endpoint string, cfg ConnectionConfig) (Client, error) {
	ep := p.getOrCreateEndpointPool(endpoint)

	select {
	case ep.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ErrConnectionTimeout
	}

	ep.mu.Lock()
	for _, pc := range ep.clients {
		if !pc.inUse && pc.client != nil && pc.client.IsAlive() {
			pc.inUse = true
			pc.lastUsed = time.Now()
			ep.mu.Unlock()
			return pc.client, nil
		}
	}
	ep.mu.Unlock()

	client, err := p.dialer.Dial(cfg)
	if err != nil {
		<-ep.sem
		return nil, err
	}

	ep.mu.Lock()
	ep.clients = append(ep.clients, &pooledClient{
		client:   client,
		lastUsed: time.Now(),
		inUse:    true,
	})
	ep.mu.Unlock()

	return client, nil
}

// Put returns a client to the pool.
func (p *Pool) Put(endpoint string, client Client) {
	p.mu.RLock()
	ep, ok := p.pools[endpoint]
	p.mu.RUnlock()

	if !ok {
		client.Close()
		return
	}

	ep.mu.Lock()
	for _, pc := range ep.clients {
		if pc.client == client {
			if !client.IsAlive() {
				pc.client.Close()
				pc.inUse = false
				pc.client = nil
			} else {
				pc.inUse = false
				pc.lastUsed = time.Now()
			}
			break
		}
	}
	ep.mu.Unlock()

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
		newClients := make([]*pooledClient, 0, len(ep.clients))
		for _, pc := range ep.clients {
			if pc.client == nil {
				continue
			}
			if !pc.inUse && now.Sub(pc.lastUsed) > p.config.IdleTimeout {
				pc.client.Close()
				select {
				case <-ep.sem:
				default:
				}
				continue
			}
			newClients = append(newClients, pc)
		}
		ep.clients = newClients
		ep.mu.Unlock()
	}
}

// Close closes all connections in the pool.
func (p *Pool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, ep := range p.pools {
		ep.mu.Lock()
		for _, pc := range ep.clients {
			if pc.client != nil {
				pc.client.Close()
			}
		}
		ep.clients = nil
		ep.mu.Unlock()
	}
	p.pools = make(map[string]*endpointPool)
	return nil
}

func (p *Pool) getOrCreateEndpointPool(endpoint string) *endpointPool {
	p.mu.RLock()
	ep, ok := p.pools[endpoint]
	p.mu.RUnlock()

	if ok {
		return ep
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if ep, ok = p.pools[endpoint]; ok {
		return ep
	}

	ep = &endpointPool{
		maxConns: p.config.MaxConnsPerEndpoint,
		sem:      make(chan struct{}, p.config.MaxConnsPerEndpoint),
	}
	p.pools[endpoint] = ep
	return ep
}
