package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// RequestOptions holds options for building an HTTP request.
type RequestOptions struct {
	Method  string
	Headers []string // "Key: Value" format
	Data    string
	Output  string // -o file path
	Silent  bool
	Verbose bool
	WriteOut string // -w format string
}

// BuildRequest creates an HTTP request from the given path/URL and options.
func (c *Client) BuildRequest(pathOrURL string, opts RequestOptions) (*http.Request, error) {
	fullURL, err := c.ResolveURL(pathOrURL)
	if err != nil {
		return nil, err
	}

	method := opts.Method
	if method == "" {
		method = http.MethodGet
	}

	var body io.Reader
	if opts.Data != "" {
		body = strings.NewReader(opts.Data)
	}

	req, err := http.NewRequest(method, fullURL, body)
	if err != nil {
		return nil, err
	}

	// Set headers
	for _, h := range opts.Headers {
		idx := strings.Index(h, ":")
		if idx > 0 {
			key := strings.TrimSpace(h[:idx])
			val := strings.TrimSpace(h[idx+1:])
			req.Header.Set(key, val)
		}
	}

	// Auto Content-Type for JSON-like data
	if opts.Data != "" && req.Header.Get("Content-Type") == "" {
		trimmed := strings.TrimSpace(opts.Data)
		if (strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) ||
			(strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) {
			req.Header.Set("Content-Type", "application/json")
		}
	}

	return req, nil
}

// EndpointsResponse matches the wardgate GET /endpoints response.
type EndpointsResponse struct {
	Endpoints []EndpointInfo `json:"endpoints"`
}

// EndpointInfo describes an available endpoint.
type EndpointInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// FetchEndpoints fetches the list of endpoints from the wardgate server.
func (c *Client) FetchEndpoints() (*EndpointsResponse, error) {
	req, err := c.BuildRequest("/endpoints", RequestOptions{Method: http.MethodGet})
	if err != nil {
		return nil, err
	}

	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GET /endpoints: %s", string(body))
	}

	var result EndpointsResponse
	if err := decodeJSON(resp.Body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func decodeJSON(r io.Reader, v interface{}) error {
	return json.NewDecoder(r).Decode(v)
}
