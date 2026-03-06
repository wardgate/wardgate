package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"

	"github.com/wardgate/wardgate/internal/approval"
	"github.com/wardgate/wardgate/internal/auth"
	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/filter"
	"github.com/wardgate/wardgate/internal/grants"
	"github.com/wardgate/wardgate/internal/policy"
	"github.com/wardgate/wardgate/internal/seal"
)

const sealedHeaderPrefix = "X-Wardgate-Sealed-"

// DefaultAllowedSealHeaders is the default whitelist of headers that can be sealed.
// Only common auth-related headers are allowed unless the operator expands the list.
var DefaultAllowedSealHeaders = []string{
	"Authorization",
	"X-Api-Key",
	"X-Auth-Token",
	"Proxy-Authorization",
}

const upstreamHeader = "X-Wardgate-Upstream"

// Proxy handles HTTP requests to an endpoint.
type Proxy struct {
	endpoint           config.Endpoint
	endpointName       string
	vault              auth.Vault
	engine             *policy.Engine
	upstream           *url.URL
	allowedUpstreams   []string
	timeout            time.Duration
	approvals          *approval.Manager
	filter             *filter.Filter
	grantStore         *grants.Store
	sealer             *seal.Sealer
	allowedSealHeaders map[string]bool
}

// SetSealer sets the sealer for decrypting sealed credential headers.
func (p *Proxy) SetSealer(s *seal.Sealer) {
	p.sealer = s
}

// SetAllowedSealHeaders sets the whitelist of headers that can be sealed.
// If headers is empty, uses the default allowed headers.
// Headers are normalized using http.CanonicalHeaderKey for case-insensitive comparison.
func (p *Proxy) SetAllowedSealHeaders(headers []string) {
	if len(headers) == 0 {
		headers = DefaultAllowedSealHeaders
	}
	p.allowedSealHeaders = make(map[string]bool, len(headers))
	for _, h := range headers {
		p.allowedSealHeaders[http.CanonicalHeaderKey(h)] = true
	}
}

func (p *Proxy) isSealHeaderAllowed(header string) bool {
	if p.allowedSealHeaders == nil {
		return false
	}
	return p.allowedSealHeaders[http.CanonicalHeaderKey(header)]
}

// SetGrantStore sets the grant store for dynamic policy overrides.
func (p *Proxy) SetGrantStore(s *grants.Store) {
	p.grantStore = s
}

// New creates a new proxy for the given endpoint.
func New(endpoint config.Endpoint, vault auth.Vault, engine *policy.Engine) *Proxy {
	var upstream *url.URL
	if endpoint.Upstream != "" {
		upstream, _ = url.Parse(endpoint.Upstream)
	}
	return &Proxy{
		endpoint:         endpoint,
		vault:            vault,
		engine:           engine,
		upstream:         upstream,
		allowedUpstreams: endpoint.AllowedUpstreams,
		timeout:          30 * time.Second,
	}
}

// NewWithName creates a new proxy with an endpoint name for approval messages.
func NewWithName(name string, endpoint config.Endpoint, vault auth.Vault, engine *policy.Engine) *Proxy {
	p := New(endpoint, vault, engine)
	p.endpointName = name
	return p
}

// SetApprovalManager sets the approval manager for ask workflows.
func (p *Proxy) SetApprovalManager(m *approval.Manager) {
	p.approvals = m
}

// SetTimeout sets the upstream request timeout.
func (p *Proxy) SetTimeout(d time.Duration) {
	p.timeout = d
}

// SetFilter sets the sensitive data filter for response filtering.
func (p *Proxy) SetFilter(f *filter.Filter) {
	p.filter = f
}

// ServeHTTP handles incoming requests.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Get rate limit key from context or header
	agentID := r.Header.Get("X-Agent-ID")
	if agentID == "" {
		agentID = r.RemoteAddr
	}

	// Check grants before static policy
	grantAllowed := false
	if p.grantStore != nil {
		scope := "endpoint:" + p.endpointName
		if g := p.grantStore.CheckHTTP(agentID, scope, r.Method, r.URL.Path); g != nil {
			grantAllowed = true
		}
	}

	if !grantAllowed {
		// Evaluate policy with rate limit key
		decision := p.engine.EvaluateWithKey(r.Method, r.URL.Path, agentID)
		if decision.Action == policy.Deny {
			http.Error(w, decision.Message, http.StatusForbidden)
			return
		}
		if decision.Action == policy.RateLimited {
			w.Header().Set("Retry-After", "60")
			http.Error(w, decision.Message, http.StatusTooManyRequests)
			return
		}
		if decision.Action == policy.Ask {
			if p.approvals == nil {
				http.Error(w, "ask action requires approval manager configuration", http.StatusServiceUnavailable)
				return
			}
			approved, err := p.approvals.RequestApproval(r.Context(), p.endpointName, r.Method, r.URL.Path, agentID)
			if err != nil {
				http.Error(w, fmt.Sprintf("approval failed: %v", err), http.StatusForbidden)
				return
			}
			if !approved {
				http.Error(w, "request denied by approver", http.StatusForbidden)
				return
			}
			// Approved - continue to proxy
		}
	}

	// Resolve upstream target
	target, err := p.resolveUpstream(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Get credential (skip for sealed — agent provides credentials)
	var cred string
	if p.endpoint.Auth.Sealed {
		hasSealedHeader := false
		for name := range r.Header {
			if strings.HasPrefix(name, sealedHeaderPrefix) {
				hasSealedHeader = true
				break
			}
		}
		if !hasSealedHeader {
			http.Error(w, "missing X-Wardgate-Sealed-* headers", http.StatusBadRequest)
			return
		}
	} else {
		cred, err = p.vault.Get(p.endpoint.Auth.CredentialEnv)
		if err != nil {
			http.Error(w, "credential error", http.StatusInternalServerError)
			return
		}
	}

	// Create reverse proxy
	proxy := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = target.Scheme
			req.URL.Host = target.Host
			req.URL.Path = target.Path + r.URL.Path
			req.Host = target.Host

			// Strip the upstream header before forwarding
			req.Header.Del(upstreamHeader)

			if p.endpoint.Auth.Sealed && p.sealer != nil {
				p.processSealedHeaders(req, r.Header)
			} else if p.endpoint.Auth.Type == "bearer" {
				req.Header.Set("Authorization", "Bearer "+cred)
			} else if p.endpoint.Auth.Type == "basic" {
				req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(cred)))
			} else if p.endpoint.Auth.Type == "header" {
				req.Header.Set(p.endpoint.Auth.Header, p.endpoint.Auth.Prefix+cred)
			}
		},
		ModifyResponse: p.modifyResponse,
		FlushInterval:  -1, // Enable immediate flushing for streaming responses
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

// resolveUpstream determines the target upstream URL from the request.
// Priority: X-Wardgate-Upstream header > static upstream config.
func (p *Proxy) resolveUpstream(r *http.Request) (*url.URL, error) {
	headerVal := r.Header.Get(upstreamHeader)

	if headerVal != "" {
		// Dynamic upstream requested - validate against allowed patterns
		if len(p.allowedUpstreams) == 0 {
			return nil, fmt.Errorf("dynamic upstream not allowed: endpoint has no allowed_upstreams configured")
		}
		if !MatchUpstream(headerVal, p.allowedUpstreams) {
			return nil, fmt.Errorf("upstream %q not in allowed list", headerVal)
		}
		u, err := url.Parse(headerVal)
		if err != nil {
			return nil, fmt.Errorf("invalid upstream URL: %w", err)
		}
		return u, nil
	}

	// Fall back to static upstream
	if p.upstream != nil {
		return p.upstream, nil
	}

	return nil, fmt.Errorf("no upstream configured and no %s header provided", upstreamHeader)
}

// processSealedHeaders decrypts X-Wardgate-Sealed-* headers and sets the real
// headers on the outgoing request. Only headers in the allowed whitelist are processed.
func (p *Proxy) processSealedHeaders(req *http.Request, incoming http.Header) {
	// Remove agent's Wardgate auth header before setting decrypted values
	req.Header.Del("Authorization")

	for name, values := range incoming {
		if !strings.HasPrefix(name, sealedHeaderPrefix) {
			continue
		}
		realHeader := strings.TrimPrefix(name, sealedHeaderPrefix)
		if !p.isSealHeaderAllowed(realHeader) {
			log.Printf("sealed header %q not in allowed list, skipping", realHeader)
			req.Header.Del(name)
			continue
		}
		req.Header.Del(realHeader) // clear before Add loop so only decrypted values remain
		for _, sealed := range values {
			plaintext, err := p.sealer.Decrypt(sealed)
			if err != nil {
				log.Printf("failed to decrypt sealed header %q: %v", realHeader, err)
				continue
			}
			req.Header.Add(realHeader, plaintext)
		}
		req.Header.Del(name)
	}
}

// modifyResponse filters sensitive data from response bodies.
func (p *Proxy) modifyResponse(resp *http.Response) error {
	// Skip if no filter configured or filter is disabled
	if p.filter == nil || !p.filter.Enabled() {
		return nil
	}

	contentType := resp.Header.Get("Content-Type")

	// Handle SSE streams — must come before isTextContent because
	// "text/event-stream" matches the "text/" prefix check.
	if isSSEContent(contentType) {
		if p.filter.SSEMode() == "passthrough" {
			return nil
		}
		// Wrap body with SSE filter reader for per-message filtering
		resp.Body = &sseFilterReader{
			reader: resp.Body,
			filter: p.filter,
		}
		// Remove Content-Length since we're streaming and size may change
		resp.Header.Del("Content-Length")
		resp.ContentLength = -1
		return nil
	}

	// Only filter text-based responses
	if !isTextContent(contentType) {
		return nil
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}

	// Scan for sensitive data
	matches := p.filter.Scan(string(body))

	// Handle based on action
	if p.filter.ShouldBlock(matches) {
		// Return error response - replace body with error message
		// Generic message — do not leak filter pattern names to clients.
		errMsg := `{"error": "response blocked"}`
		resp.Body = io.NopCloser(bytes.NewBufferString(errMsg))
		resp.ContentLength = int64(len(errMsg))
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(errMsg)))
		resp.StatusCode = http.StatusForbidden
		resp.Status = "403 Forbidden"
		return nil
	}

	// Apply redaction if any matches found
	if len(matches) > 0 {
		filtered := p.filter.Apply(string(body), matches)
		resp.Body = io.NopCloser(bytes.NewBufferString(filtered))
		resp.ContentLength = int64(len(filtered))
		resp.Header.Set("Content-Length", fmt.Sprintf("%d", len(filtered)))
	} else {
		resp.Body = io.NopCloser(bytes.NewBuffer(body))
	}

	return nil
}

// isSSEContent checks if the content type is a Server-Sent Events stream.
func isSSEContent(contentType string) bool {
	return strings.Contains(strings.ToLower(contentType), "text/event-stream")
}

// isTextContent checks if the content type is text-based and should be filtered.
func isTextContent(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "text/") ||
		strings.Contains(ct, "application/json") ||
		strings.Contains(ct, "application/xml") ||
		strings.Contains(ct, "application/javascript")
}
