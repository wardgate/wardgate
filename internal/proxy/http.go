package proxy

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/wardgate/wardgate/internal/auth"
	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/policy"
)

// Proxy handles HTTP requests to an endpoint.
type Proxy struct {
	endpoint config.Endpoint
	vault    auth.Vault
	engine   *policy.Engine
	upstream *url.URL
	timeout  time.Duration
}

// New creates a new proxy for the given endpoint.
func New(endpoint config.Endpoint, vault auth.Vault, engine *policy.Engine) *Proxy {
	upstream, _ := url.Parse(endpoint.Upstream)
	return &Proxy{
		endpoint: endpoint,
		vault:    vault,
		engine:   engine,
		upstream: upstream,
		timeout:  30 * time.Second,
	}
}

// SetTimeout sets the upstream request timeout.
func (p *Proxy) SetTimeout(d time.Duration) {
	p.timeout = d
}

// ServeHTTP handles incoming requests.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Evaluate policy
	decision := p.engine.Evaluate(r.Method, r.URL.Path)
	if decision.Action == policy.Deny {
		http.Error(w, decision.Message, http.StatusForbidden)
		return
	}

	// Get credential
	cred, err := p.vault.Get(p.endpoint.Auth.CredentialEnv)
	if err != nil {
		http.Error(w, "credential error", http.StatusInternalServerError)
		return
	}

	// Create reverse proxy
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = p.upstream.Scheme
			req.URL.Host = p.upstream.Host
			req.URL.Path = p.upstream.Path + r.URL.Path
			req.Host = p.upstream.Host

			// Inject credential (strip agent auth first)
			if p.endpoint.Auth.Type == "bearer" {
				req.Header.Set("Authorization", "Bearer "+cred)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if ctx := r.Context(); ctx.Err() == context.DeadlineExceeded {
				http.Error(w, "upstream timeout", http.StatusGatewayTimeout)
				return
			}
			http.Error(w, fmt.Sprintf("upstream error: %v", err), http.StatusBadGateway)
		},
	}

	// Apply timeout
	ctx, cancel := context.WithTimeout(r.Context(), p.timeout)
	defer cancel()
	r = r.WithContext(ctx)

	proxy.ServeHTTP(w, r)
}
