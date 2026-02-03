package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/wardgate/wardgate/internal/approval"
	"github.com/wardgate/wardgate/internal/audit"
	"github.com/wardgate/wardgate/internal/auth"
	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/notify"
	"github.com/wardgate/wardgate/internal/policy"
	"github.com/wardgate/wardgate/internal/proxy"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to config file")
	envPath := flag.String("env", ".env", "Path to .env file")
	flag.Parse()

	// Load environment variables
	if err := godotenv.Load(*envPath); err != nil {
		log.Printf("Warning: could not load %s: %v", *envPath, err)
	}

	// Load configuration
	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Setup components
	vault := auth.NewEnvVault()
	auditLog := audit.New(os.Stdout)

	// Setup approval manager if notifications are configured
	var approvalMgr *approval.Manager
	if cfg.Notify.Slack != nil || cfg.Notify.Webhook != nil {
		timeout := 5 * time.Minute
		if cfg.Notify.Timeout != "" {
			if d, err := time.ParseDuration(cfg.Notify.Timeout); err == nil {
				timeout = d
			}
		}

		baseURL := cfg.Server.ApprovalURL
		if baseURL == "" {
			baseURL = "http://localhost" + cfg.Server.Listen
		}

		approvalMgr = approval.NewManager(baseURL, timeout)

		// Add notification channels
		if cfg.Notify.Slack != nil && cfg.Notify.Slack.WebhookURL != "" {
			approvalMgr.AddNotifier(notify.NewSlackChannel(cfg.Notify.Slack.WebhookURL))
			log.Printf("Slack notifications enabled")
		}
		if cfg.Notify.Webhook != nil && cfg.Notify.Webhook.URL != "" {
			approvalMgr.AddNotifier(notify.NewWebhookChannel(cfg.Notify.Webhook.URL, cfg.Notify.Webhook.Headers))
			log.Printf("Webhook notifications enabled")
		}
	}

	// Create mux for API endpoints (requires agent auth)
	apiMux := http.NewServeMux()

	// Register endpoint proxies
	for name, endpoint := range cfg.Endpoints {
		engine := policy.New(endpoint.Rules)
		p := proxy.NewWithName(name, endpoint, vault, engine)
		if approvalMgr != nil {
			p.SetApprovalManager(approvalMgr)
		}

		h := auditMiddleware(auditLog, name, p)
		apiMux.Handle("/"+name+"/", http.StripPrefix("/"+name, h))
		log.Printf("Registered endpoint: /%s/ -> %s", name, endpoint.Upstream)
	}

	// Wrap API endpoints with agent authentication
	authedAPI := auth.NewAgentAuthMiddleware(cfg.Agents, apiMux)

	// Create root mux - approval endpoints are public (token-protected)
	rootMux := http.NewServeMux()

	// Register public approval endpoints (no auth required, token in URL provides security)
	if approvalMgr != nil {
		rootMux.Handle("/approve/", approvalMgr.Handler())
		rootMux.Handle("/deny/", approvalMgr.Handler())
		rootMux.Handle("/status/", approvalMgr.Handler())
	}

	// All other requests go through agent auth
	rootMux.Handle("/", authedAPI)

	handler := http.Handler(rootMux)

	// Start server
	log.Printf("Wardgate listening on %s", cfg.Server.Listen)
	if err := http.ListenAndServe(cfg.Server.Listen, handler); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}

func auditMiddleware(logger *audit.Logger, endpoint string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		logger.Log(audit.Entry{
			RequestID:      r.Header.Get("X-Request-ID"),
			Endpoint:       endpoint,
			Method:         r.Method,
			Path:           r.URL.Path,
			SourceIP:       r.RemoteAddr,
			AgentID:        r.Header.Get("X-Agent-ID"),
			Decision:       decisionFromStatus(rw.status),
			UpstreamStatus: rw.status,
			ResponseBytes:  rw.bytes,
			DurationMs:     time.Since(start).Milliseconds(),
		})
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += int64(n)
	return n, err
}

func decisionFromStatus(status int) string {
	switch {
	case status == http.StatusForbidden:
		return "deny"
	case status == http.StatusTooManyRequests:
		return "rate_limited"
	case status >= 200 && status < 400:
		return "allow"
	default:
		return "error"
	}
}
