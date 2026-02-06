package cli

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// ClientOptions configures the HTTP client.
type ClientOptions struct {
	FollowRedirects    bool         // -L: follow same-host redirects only
	InsecureSkipVerify bool         // -k: skip TLS verification
	RootCAs            *x509.CertPool // Custom CA pool (e.g. from ca_file config)
}

// Client is an HTTP client restricted to the configured wardgate server.
type Client struct {
	baseURL   *url.URL
	key       string
	options   ClientOptions
	transport *http.Transport
}

// NewClient creates a client for the given server URL.
func NewClient(serverURL, key string, opts ClientOptions) (*Client, error) {
	u, err := url.Parse(serverURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("server URL must include scheme and host (e.g. http://wardgate:8080)")
	}
	// Normalize: no trailing path
	u.Path = ""
	u.RawPath = ""
	u.RawQuery = ""

	tlsConfig := &tls.Config{}
	if opts.InsecureSkipVerify {
		tlsConfig.InsecureSkipVerify = true
	}
	if opts.RootCAs != nil {
		tlsConfig.RootCAs = opts.RootCAs
	}

	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	return &Client{
		baseURL:   u,
		key:       key,
		options:   opts,
		transport: transport,
	}, nil
}

// ResolveURL resolves a path or URL to a full URL. If path is relative (starts
// with /), it's resolved against the configured server. If it's an absolute
// URL, it must match the configured server exactly.
func (c *Client) ResolveURL(pathOrURL string) (string, error) {
	if strings.HasPrefix(pathOrURL, "/") {
		// Relative path
		return c.baseURL.String() + pathOrURL, nil
	}
	// Absolute URL - validate it matches our server
	u, err := url.Parse(pathOrURL)
	if err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}
	if !c.matchesServer(u) {
		return "", fmt.Errorf("URL %s does not match configured server %s", pathOrURL, c.baseURL.String())
	}
	return pathOrURL, nil
}

func (c *Client) matchesServer(u *url.URL) bool {
	return u.Scheme == c.baseURL.Scheme &&
		u.Host == c.baseURL.Host &&
		strings.HasPrefix(u.Path, "/")
}

// Do sends an HTTP request. The request URL (from req.URL) must match the
// configured server. Adds Authorization: Bearer <key>.
func (c *Client) Do(req *http.Request) (*http.Response, error) {
	if !c.matchesServer(req.URL) {
		return nil, fmt.Errorf("request URL %s does not match configured server %s", req.URL.String(), c.baseURL.String())
	}
	req.Header.Set("Authorization", "Bearer "+c.key)

	client := &http.Client{Transport: c.transport}
	if c.options.FollowRedirects {
		client.CheckRedirect = c.checkRedirect
	} else {
		client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}

	return client.Do(req)
}

func (c *Client) checkRedirect(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return fmt.Errorf("too many redirects")
	}
	// Only follow redirects to same host
	if req.URL.Scheme != c.baseURL.Scheme || req.URL.Host != c.baseURL.Host {
		return fmt.Errorf("redirect to %s rejected: only same-host redirects allowed", req.URL.String())
	}
	return nil
}
