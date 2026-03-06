package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/wardgate/wardgate/internal/cli"
	"gopkg.in/yaml.v3"
)

var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

// Config holds wardgate-proxy configuration.
type Config struct {
	Server string `yaml:"server"`
	Key    string `yaml:"key"`
	KeyEnv string `yaml:"key_env"`
	Listen string `yaml:"listen"`
	CAFile string `yaml:"ca_file"`
}

// KeyReader returns the current agent key.
type KeyReader interface {
	Read() (string, error)
}

// staticKeyReader holds a resolved key for the process lifetime (key_env mode).
type staticKeyReader struct {
	key string
}

func (s *staticKeyReader) Read() (string, error) {
	return s.key, nil
}

// configKeyReader watches the config file's mtime and re-reads the key field
// on change. On re-read failure, it falls back to the cached key.
type configKeyReader struct {
	configPath string
	mu         sync.RWMutex
	key        string
	mtime      time.Time
}

func newConfigKeyReader(configPath string) *configKeyReader {
	return &configKeyReader{configPath: configPath}
}

// Read returns the current key, re-reading from the config file if mtime changed.
func (cr *configKeyReader) Read() (string, error) {
	info, err := os.Stat(cr.configPath)
	if err != nil {
		cr.mu.RLock()
		if cr.key != "" {
			defer cr.mu.RUnlock()
			log.Printf("Warning: cannot stat config file, using cached key: %v", err)
			return cr.key, nil
		}
		cr.mu.RUnlock()
		return "", fmt.Errorf("stat config file: %w", err)
	}

	cr.mu.RLock()
	if cr.key != "" && info.ModTime().Equal(cr.mtime) {
		defer cr.mu.RUnlock()
		return cr.key, nil
	}
	cr.mu.RUnlock()

	cr.mu.Lock()
	defer cr.mu.Unlock()

	// Double-check after acquiring write lock.
	info2, err := os.Stat(cr.configPath)
	if err != nil {
		if cr.key != "" {
			log.Printf("Warning: cannot stat config file, using cached key: %v", err)
			return cr.key, nil
		}
		return "", fmt.Errorf("stat config file: %w", err)
	}
	if cr.key != "" && info2.ModTime().Equal(cr.mtime) {
		return cr.key, nil
	}

	data, err := os.ReadFile(cr.configPath)
	if err != nil {
		if cr.key != "" {
			log.Printf("Warning: cannot read config file, using cached key: %v", err)
			return cr.key, nil
		}
		return "", fmt.Errorf("read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		if cr.key != "" {
			log.Printf("Warning: cannot parse config file, using cached key: %v", err)
			return cr.key, nil
		}
		return "", fmt.Errorf("parse config file: %w", err)
	}

	key := strings.TrimSpace(cfg.Key)
	if key == "" {
		if cr.key != "" {
			log.Printf("Warning: config file has empty key, using cached key")
			return cr.key, nil
		}
		return "", fmt.Errorf("config file has empty key")
	}

	cr.key = key
	cr.mtime = info2.ModTime()
	return key, nil
}

func buildTransport(caFile string) (*http.Transport, error) {
	cliCfg := &cli.Config{CAFile: caFile}
	rootCAs, err := cliCfg.LoadRootCAs()
	if err != nil {
		return nil, err
	}
	return &http.Transport{TLSClientConfig: &tls.Config{RootCAs: rootCAs}}, nil
}

// NewProxyHandler builds an http.Handler that reads an agent key from keyReader
// and forwards every request to upstream with that key injected.
func NewProxyHandler(upstream *url.URL, keyReader KeyReader, transport http.RoundTripper) http.Handler {
	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			key := req.Context().Value(ctxAgentKey).(string)

			req.URL.Scheme = upstream.Scheme
			req.URL.Host = upstream.Host
			req.Host = upstream.Host
			req.Header.Set("Authorization", "Bearer "+key)
			req.Header.Del("X-Forwarded-For")
		},
		FlushInterval: -1, // flush immediately for SSE and streaming
		Transport:     transport,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, fmt.Sprintf("proxy error: %v", err), http.StatusBadGateway)
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key, err := keyReader.Read()
		if err != nil {
			http.Error(w, fmt.Sprintf("key error: %v", err), http.StatusBadGateway)
			return
		}
		ctx := context.WithValue(r.Context(), ctxAgentKey, key)
		rp.ServeHTTP(w, r.WithContext(ctx))
	})
}

type contextKey string

const ctxAgentKey contextKey = "agentKey"

// resolveConfig loads the config file and applies flag overrides.
func resolveConfig(configPath string, configExplicit bool, listen, server, keyEnv string) (*Config, error) {
	cfg := &Config{Listen: "127.0.0.1:18080"}
	data, err := os.ReadFile(configPath)
	if err != nil {
		if configExplicit {
			return nil, fmt.Errorf("config file %s not found", configPath)
		}
	} else {
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parsing config %s: %w", configPath, err)
		}
	}

	if listen != "" {
		cfg.Listen = listen
	}
	if server != "" {
		cfg.Server = server
	}
	if keyEnv != "" {
		cfg.KeyEnv = keyEnv
	}

	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:18080"
	}
	if cfg.Server == "" {
		return nil, fmt.Errorf("server URL not configured (set server in config or use -server flag)")
	}
	if cfg.Key == "" && cfg.KeyEnv == "" {
		return nil, fmt.Errorf("agent key not configured (set key or key_env in config, or use -key-env flag)")
	}
	return cfg, nil
}

func main() {
	configPath := flag.String("config", "wardgate-proxy.yaml", "Path to config file")
	listenFlag := flag.String("listen", "", "Listen address (overrides config)")
	serverFlag := flag.String("server", "", "Wardgate server URL (overrides config)")
	keyEnvFlag := flag.String("key-env", "", "Environment variable containing agent key (overrides config)")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("wardgate-proxy %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	configExplicit := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			configExplicit = true
		}
	})

	cfg, err := resolveConfig(*configPath, configExplicit, *listenFlag, *serverFlag, *keyEnvFlag)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	upstream, err := url.Parse(cfg.Server)
	if err != nil {
		log.Fatalf("Error: invalid server URL: %v", err)
	}
	if upstream.Scheme == "" || upstream.Host == "" {
		log.Fatal("Error: server URL must include scheme and host (e.g. https://gateway.example.com)")
	}

	transport, err := buildTransport(cfg.CAFile)
	if err != nil {
		log.Fatalf("Error: %v", err)
	}

	// Create the appropriate key reader.
	var keyReader KeyReader
	if cfg.Key != "" {
		keyReader = newConfigKeyReader(*configPath)
	} else {
		// cfg.KeyEnv is set (validated by resolveConfig).
		resolved := os.Getenv(cfg.KeyEnv)
		if resolved == "" {
			log.Fatalf("Error: environment variable %s is empty", cfg.KeyEnv)
		}
		keyReader = &staticKeyReader{key: resolved}
	}

	// Validate the key is readable at startup.
	if _, err := keyReader.Read(); err != nil {
		log.Fatalf("Error: cannot read agent key: %v", err)
	}

	handler := NewProxyHandler(upstream, keyReader, transport)

	srv := &http.Server{
		Addr:    cfg.Listen,
		Handler: handler,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("wardgate-proxy listening on %s -> %s", cfg.Listen, cfg.Server)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Error: %v", err)
		}
	}()

	<-done
	log.Println("Shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Error during shutdown: %v", err)
	}
}
