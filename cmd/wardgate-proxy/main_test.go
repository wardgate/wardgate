package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// --- staticKeyReader tests ---

func TestStaticKeyReader(t *testing.T) {
	r := &staticKeyReader{key: "my-key"}
	key, err := r.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "my-key" {
		t.Fatalf("got %q, want %q", key, "my-key")
	}

	// Second read returns the same value.
	key2, err := r.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key2 != "my-key" {
		t.Fatalf("got %q, want %q", key2, "my-key")
	}
}

// --- configKeyReader tests ---

func TestConfigKeyReader_ReadsKey(t *testing.T) {
	path := writeKeyFile(t, "my-agent-key")
	cr := newConfigKeyReader(path)

	key, err := cr.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "my-agent-key" {
		t.Fatalf("got %q, want %q", key, "my-agent-key")
	}
}

func TestConfigKeyReader_TrimsWhitespace(t *testing.T) {
	path := writeKeyFile(t, "  key-with-spaces ")
	cr := newConfigKeyReader(path)

	key, err := cr.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if key != "key-with-spaces" {
		t.Fatalf("got %q, want %q", key, "key-with-spaces")
	}
}

func TestConfigKeyReader_EmptyKeyError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	os.WriteFile(path, []byte("server: https://example.com\nkey: \"\"\n"), 0o600)

	cr := newConfigKeyReader(path)
	_, err := cr.Read()
	if err == nil {
		t.Fatal("expected error for empty key")
	}
	if !strings.Contains(err.Error(), "empty key") {
		t.Fatalf("expected empty key error, got: %v", err)
	}
}

func TestConfigKeyReader_MissingFileError(t *testing.T) {
	cr := newConfigKeyReader("/nonexistent/config.yaml")

	_, err := cr.Read()
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestConfigKeyReader_CachesByMtime(t *testing.T) {
	path := writeKeyFile(t, "key-v1")
	cr := newConfigKeyReader(path)

	k1, err := cr.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k1 != "key-v1" {
		t.Fatalf("got %q, want %q", k1, "key-v1")
	}

	// Record the original mtime.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	origMtime := info.ModTime()

	// Overwrite file with different content, then restore original mtime.
	writeKeyFileAt(t, path, "key-v2-should-not-see")
	os.Chtimes(path, origMtime, origMtime)

	// Read again -- same mtime means the cache should return "key-v1".
	k2, err := cr.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k2 != "key-v1" {
		t.Fatalf("got %q, want %q (cache was not used)", k2, "key-v1")
	}
}

func TestConfigKeyReader_DetectsRotation(t *testing.T) {
	path := writeKeyFile(t, "key-v1")
	cr := newConfigKeyReader(path)

	k1, _ := cr.Read()
	if k1 != "key-v1" {
		t.Fatalf("got %q, want %q", k1, "key-v1")
	}

	// Ensure mtime changes (some filesystems have 1s resolution).
	time.Sleep(10 * time.Millisecond)
	writeKeyFileAt(t, path, "key-v2")

	// Force mtime to be different.
	future := time.Now().Add(2 * time.Second)
	os.Chtimes(path, future, future)

	k2, _ := cr.Read()
	if k2 != "key-v2" {
		t.Fatalf("got %q, want %q", k2, "key-v2")
	}
}

func TestConfigKeyReader_FallbackOnPartialWrite(t *testing.T) {
	path := writeKeyFile(t, "good-key")
	cr := newConfigKeyReader(path)

	// First read succeeds and caches.
	k1, err := cr.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k1 != "good-key" {
		t.Fatalf("got %q, want %q", k1, "good-key")
	}

	// Simulate a half-written file: valid YAML but empty key.
	os.WriteFile(path, []byte("server: https://example.com\nkey: \"\"\n"), 0o600)
	future := time.Now().Add(2 * time.Second)
	os.Chtimes(path, future, future)

	// Should fall back to cached key.
	k2, err := cr.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k2 != "good-key" {
		t.Fatalf("got %q, want %q (expected fallback to cached key)", k2, "good-key")
	}

	// Simulate invalid YAML.
	os.WriteFile(path, []byte("{{{invalid"), 0o600)
	future2 := time.Now().Add(4 * time.Second)
	os.Chtimes(path, future2, future2)

	// Should still fall back to cached key.
	k3, err := cr.Read()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if k3 != "good-key" {
		t.Fatalf("got %q, want %q (expected fallback to cached key)", k3, "good-key")
	}
}

func TestConfigKeyReader_ConcurrentAccess(t *testing.T) {
	path := writeKeyFile(t, "concurrent-key")
	cr := newConfigKeyReader(path)

	var wg sync.WaitGroup
	errs := make(chan error, 50)
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key, err := cr.Read()
			if err != nil {
				errs <- err
				return
			}
			if key != "concurrent-key" {
				errs <- fmt.Errorf("goroutine %d: got %q", i, key)
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}

// --- Config loading tests ---

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}

func TestLoadConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
server: https://gateway.example.com
key: my-agent-key
listen: 127.0.0.1:9090
ca_file: /etc/ca.pem
`), 0o600)

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Server != "https://gateway.example.com" {
		t.Errorf("server: got %q", cfg.Server)
	}
	if cfg.Key != "my-agent-key" {
		t.Errorf("key: got %q", cfg.Key)
	}
	if cfg.Listen != "127.0.0.1:9090" {
		t.Errorf("listen: got %q", cfg.Listen)
	}
	if cfg.CAFile != "/etc/ca.pem" {
		t.Errorf("ca_file: got %q", cfg.CAFile)
	}
}

func TestLoadConfig_KeyEnv(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgPath, []byte(`
server: https://gateway.example.com
key_env: WARDGATE_AGENT_KEY
`), 0o600)

	cfg, err := loadConfig(cfgPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.KeyEnv != "WARDGATE_AGENT_KEY" {
		t.Errorf("key_env: got %q", cfg.KeyEnv)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	_, err := loadConfig("/nonexistent/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing config")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "bad.yaml")
	os.WriteFile(cfgPath, []byte(`{{{invalid`), 0o600)

	_, err := loadConfig(cfgPath)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

// --- Proxy handler unit tests ---

func TestProxyHandler_InjectsBearer(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-agent-key" {
			t.Errorf("got Authorization %q, want %q", auth, "Bearer test-agent-key")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, &staticKeyReader{key: "test-agent-key"}, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/some/path")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("got status %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("got body %q, want %q", string(body), "ok")
	}
}

func TestProxyHandler_ForwardsPath(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, &staticKeyReader{key: "tok"}, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	http.Get(proxy.URL + "/github/repos?page=2")

	if gotPath != "/github/repos" {
		t.Errorf("got path %q, want %q", gotPath, "/github/repos")
	}
}

func TestProxyHandler_ForwardsQueryParams(t *testing.T) {
	var gotQuery string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, &staticKeyReader{key: "tok"}, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	http.Get(proxy.URL + "/endpoint?key=value&foo=bar")

	if gotQuery != "key=value&foo=bar" {
		t.Errorf("got query %q, want %q", gotQuery, "key=value&foo=bar")
	}
}

func TestProxyHandler_ForwardsMethod(t *testing.T) {
	var gotMethod string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, &staticKeyReader{key: "tok"}, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	req, _ := http.NewRequest(http.MethodDelete, proxy.URL+"/resource/123", nil)
	http.DefaultClient.Do(req)

	if gotMethod != http.MethodDelete {
		t.Errorf("got method %q, want DELETE", gotMethod)
	}
}

func TestProxyHandler_ForwardsRequestBody(t *testing.T) {
	var gotBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, &staticKeyReader{key: "tok"}, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	http.Post(proxy.URL+"/data", "application/json", strings.NewReader(`{"key":"value"}`))

	if gotBody != `{"key":"value"}` {
		t.Errorf("got body %q", gotBody)
	}
}

func TestProxyHandler_ForwardsCustomHeaders(t *testing.T) {
	var gotHeader string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Custom-Header")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, &staticKeyReader{key: "tok"}, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	req, _ := http.NewRequest(http.MethodGet, proxy.URL+"/test", nil)
	req.Header.Set("X-Custom-Header", "custom-value")
	http.DefaultClient.Do(req)

	if gotHeader != "custom-value" {
		t.Errorf("got header %q, want %q", gotHeader, "custom-value")
	}
}

func TestProxyHandler_StripsAgentAuth(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, &staticKeyReader{key: "proxy-key"}, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	req, _ := http.NewRequest(http.MethodGet, proxy.URL+"/test", nil)
	req.Header.Set("Authorization", "Bearer agent-should-be-overwritten")
	http.DefaultClient.Do(req)

	if gotAuth != "Bearer proxy-key" {
		t.Errorf("got auth %q, want %q", gotAuth, "Bearer proxy-key")
	}
}

func TestProxyHandler_Returns502OnKeyError(t *testing.T) {
	cr := newConfigKeyReader("/nonexistent/config.yaml")
	u, _ := url.Parse("http://localhost:1")
	handler := NewProxyHandler(u, cr, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("got status %d, want 502", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "key error") {
		t.Errorf("expected key error in body, got: %s", body)
	}
}

func TestProxyHandler_Returns502OnUpstreamDown(t *testing.T) {
	u, _ := url.Parse("http://127.0.0.1:1") // nothing listening
	handler := NewProxyHandler(u, &staticKeyReader{key: "valid-key"}, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("got status %d, want 502", resp.StatusCode)
	}
}

func TestProxyHandler_ReturnsUpstreamStatus(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("denied"))
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, &staticKeyReader{key: "tok"}, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/test")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("got status %d, want 403", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "denied" {
		t.Errorf("got body %q, want %q", string(body), "denied")
	}
}

// --- SSE / Streaming tests ---

func TestProxyHandler_StreamsSSE(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("server does not support flushing")
		}
		for i := range 3 {
			fmt.Fprintf(w, "data: event-%d\n\n", i)
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, &staticKeyReader{key: "sse-key"}, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/events")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Errorf("got content-type %q, want text/event-stream", resp.Header.Get("Content-Type"))
	}

	body, _ := io.ReadAll(resp.Body)
	for i := range 3 {
		expected := fmt.Sprintf("data: event-%d", i)
		if !strings.Contains(string(body), expected) {
			t.Errorf("missing SSE event %q in body: %s", expected, body)
		}
	}
}

func TestProxyHandler_StreamsChunked(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("no flusher")
		}
		for i := range 5 {
			fmt.Fprintf(w, "chunk-%d\n", i)
			flusher.Flush()
			time.Sleep(5 * time.Millisecond)
		}
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, &staticKeyReader{key: "stream-key"}, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/stream")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	for i := range 5 {
		expected := fmt.Sprintf("chunk-%d", i)
		if !strings.Contains(string(body), expected) {
			t.Errorf("missing chunk %q in body: %s", expected, body)
		}
	}
}

// --- Key rotation e2e test ---

func TestE2E_KeyRotation(t *testing.T) {
	var receivedKeys []string
	var mu sync.Mutex

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		receivedKeys = append(receivedKeys, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	path := writeKeyFile(t, "key-v1")
	cr := newConfigKeyReader(path)
	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, cr, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	// Request with key-v1
	resp, _ := http.Get(proxy.URL + "/test")
	resp.Body.Close()

	// Rotate key
	writeKeyFileAt(t, path, "key-v2")
	future := time.Now().Add(2 * time.Second)
	os.Chtimes(path, future, future)

	// Request with key-v2
	resp, _ = http.Get(proxy.URL + "/test")
	resp.Body.Close()

	// Rotate again
	writeKeyFileAt(t, path, "key-v3")
	future2 := time.Now().Add(4 * time.Second)
	os.Chtimes(path, future2, future2)

	// Request with key-v3
	resp, _ = http.Get(proxy.URL + "/test")
	resp.Body.Close()

	mu.Lock()
	defer mu.Unlock()

	expected := []string{"Bearer key-v1", "Bearer key-v2", "Bearer key-v3"}
	if len(receivedKeys) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(receivedKeys))
	}
	for i, want := range expected {
		if receivedKeys[i] != want {
			t.Errorf("request %d: got %q, want %q", i, receivedKeys[i], want)
		}
	}
}

// --- Full e2e test with real listener ---

func TestE2E_FullProxyLifecycle(t *testing.T) {
	var requestCount atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		w.Header().Set("X-Upstream", "true")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, &staticKeyReader{key: "e2e-agent-key"}, http.DefaultTransport)

	// Start on a random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	proxyAddr := listener.Addr().String()

	srv := &http.Server{Handler: handler}
	go srv.Serve(listener)
	defer srv.Close()

	// Multiple requests through the proxy
	for range 5 {
		resp, err := http.Get("http://" + proxyAddr + "/api/test")
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("got status %d", resp.StatusCode)
		}
		if resp.Header.Get("X-Upstream") != "true" {
			t.Error("missing upstream response header")
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if string(body) != `{"status":"ok"}` {
			t.Errorf("got body %q", string(body))
		}
	}

	if requestCount.Load() != 5 {
		t.Errorf("expected 5 upstream requests, got %d", requestCount.Load())
	}
}

// --- E2E: concurrent requests during key rotation ---

func TestE2E_ConcurrentRequestsDuringRotation(t *testing.T) {
	var mu sync.Mutex
	keys := make(map[string]int)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		keys[r.Header.Get("Authorization")]++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	path := writeKeyFile(t, "initial-key")
	cr := newConfigKeyReader(path)
	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, cr, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	var wg sync.WaitGroup

	// Fire 20 requests, rotate the key midway.
	for i := range 20 {
		if i == 10 {
			writeKeyFileAt(t, path, "rotated-key")
			future := time.Now().Add(2 * time.Second)
			os.Chtimes(path, future, future)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			resp, err := http.Get(proxy.URL + "/test")
			if err != nil {
				t.Errorf("request failed: %v", err)
				return
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("got status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()

	// All requests should have used one of the two keys.
	total := 0
	for auth, count := range keys {
		if auth != "Bearer initial-key" && auth != "Bearer rotated-key" {
			t.Errorf("unexpected key: %q", auth)
		}
		total += count
	}
	if total != 20 {
		t.Errorf("expected 20 total requests, got %d", total)
	}
}

// --- E2E: SSE streaming with key injection ---

func TestE2E_SSEWithKeyInjection(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer sse-key" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		flusher := w.(http.Flusher)
		for i := range 3 {
			fmt.Fprintf(w, "id: %d\ndata: message-%d\n\n", i, i)
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, &staticKeyReader{key: "sse-key"}, http.DefaultTransport)

	listener, _ := net.Listen("tcp", "127.0.0.1:0")
	srv := &http.Server{Handler: handler}
	go srv.Serve(listener)
	defer srv.Close()

	resp, err := http.Get("http://" + listener.Addr().String() + "/events")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("got status %d, want 200", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	for i := range 3 {
		if !strings.Contains(string(body), fmt.Sprintf("data: message-%d", i)) {
			t.Errorf("missing event %d in SSE stream", i)
		}
	}
}

// --- E2E: large response body ---

func TestE2E_LargeResponseBody(t *testing.T) {
	largeBody := strings.Repeat("x", 1024*1024) // 1MB

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(largeBody))
	}))
	defer upstream.Close()

	u, _ := url.Parse(upstream.URL)
	handler := NewProxyHandler(u, &staticKeyReader{key: "tok"}, http.DefaultTransport)
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	resp, err := http.Get(proxy.URL + "/large")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if len(body) != len(largeBody) {
		t.Errorf("got %d bytes, want %d", len(body), len(largeBody))
	}
}

// --- buildTransport tests ---

func TestBuildTransport_NoCA(t *testing.T) {
	tr, err := buildTransport("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tr.TLSClientConfig.RootCAs != nil {
		t.Error("expected nil RootCAs when no ca_file")
	}
}

func TestBuildTransport_InvalidCAFile(t *testing.T) {
	_, err := buildTransport("/nonexistent/ca.pem")
	if err == nil {
		t.Fatal("expected error for missing CA file")
	}
}

func TestBuildTransport_InvalidPEM(t *testing.T) {
	dir := t.TempDir()
	caPath := filepath.Join(dir, "bad.pem")
	os.WriteFile(caPath, []byte("not-a-certificate"), 0o600)

	_, err := buildTransport(caPath)
	if err == nil {
		t.Fatal("expected error for invalid PEM")
	}
}

// --- Helpers ---

// writeKeyFile writes a config YAML with the given key and returns the path.
func writeKeyFile(t *testing.T, key string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	content := fmt.Sprintf("server: https://example.com\nkey: %q\n", key)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeKeyFileAt overwrites an existing config file with the given key.
func writeKeyFileAt(t *testing.T, path, key string) {
	t.Helper()
	content := fmt.Sprintf("server: https://example.com\nkey: %q\n", key)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
