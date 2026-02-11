package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/wardgate/wardgate/internal/approval"
	"github.com/wardgate/wardgate/internal/audit"
	"github.com/wardgate/wardgate/internal/auth"
	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/discovery"
	execpkg "github.com/wardgate/wardgate/internal/exec"
	"github.com/wardgate/wardgate/internal/imap"
	"github.com/wardgate/wardgate/internal/notify"
	"github.com/wardgate/wardgate/internal/policy"
	"github.com/wardgate/wardgate/internal/hub"
	"github.com/wardgate/wardgate/internal/proxy"
	"github.com/wardgate/wardgate/internal/smtp"
)

// Set by goreleaser ldflags
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func main() {
	// Check for CLI subcommands before parsing flags
	if len(os.Args) > 1 && os.Args[1] == "approvals" {
		runCLI(os.Args[1:])
		return
	}

	configPath := flag.String("config", "config.yaml", "Path to config file")
	envPath := flag.String("env", ".env", "Path to .env file")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("wardgate %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

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

	// Setup log store for dashboard
	logStoreCapacity := 1000
	if cfg.Server.Logging.MaxEntries > 0 {
		logStoreCapacity = cfg.Server.Logging.MaxEntries
	}
	logStore := audit.NewStore(logStoreCapacity)
	auditLog.SetStore(logStore)
	auditLog.SetStoreBodies(cfg.Server.Logging.StoreBodies)

	// Setup approval manager if notifications are configured
	var approvalMgr *approval.Manager
	if cfg.Notify.Slack != nil || cfg.Notify.Webhook != nil {
		timeout := 5 * time.Minute
		if cfg.Notify.Timeout != "" {
			if d, err := time.ParseDuration(cfg.Notify.Timeout); err == nil {
				timeout = d
			}
		}

		baseURL := cfg.Server.BaseURL
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

	// Create shared IMAP pool for IMAP endpoints
	imapDialer := imap.NewIMAPDialer()
	imapPool := imap.NewPool(imapDialer, imap.PoolConfig{
		MaxConnsPerEndpoint: 5,
		IdleTimeout:         5 * time.Minute,
	})

	// Register endpoint proxies
	for name, endpoint := range cfg.Endpoints {
		engine := policy.New(endpoint.Rules)

		var h http.Handler

		adapter := strings.ToLower(endpoint.Adapter)
		if adapter == "" {
			adapter = "http"
		}

		switch adapter {
		case "imap":
			// Parse IMAP connection details from upstream URL
			connCfg, err := parseIMAPUpstream(endpoint, vault)
			if err != nil {
				log.Fatalf("Failed to parse IMAP config for %s: %v", name, err)
			}

			imapHandler := imap.NewHandler(imapPool, engine, imap.HandlerConfig{
				EndpointName:     name,
				ConnectionConfig: connCfg,
			})
			h = auditMiddleware(auditLog, name, imapHandler)
			log.Printf("Registered IMAP endpoint: /%s/ -> %s:%d", name, connCfg.Host, connCfg.Port)

		case "smtp":
			// Parse SMTP connection details from upstream URL
			smtpConnCfg, err := parseSMTPUpstream(endpoint, vault)
			if err != nil {
				log.Fatalf("Failed to parse SMTP config for %s: %v", name, err)
			}

			smtpClient := smtp.NewSMTPClient(smtpConnCfg)
			smtpHandlerCfg := smtp.HandlerConfig{
				EndpointName: name,
				From:         smtpConnCfg.From,
			}
			if endpoint.SMTP != nil {
				smtpHandlerCfg.AllowedRecipients = endpoint.SMTP.AllowedRecipients
				smtpHandlerCfg.KnownRecipients = endpoint.SMTP.KnownRecipients
				smtpHandlerCfg.AskNewRecipients = endpoint.SMTP.AskNewRecipients
				smtpHandlerCfg.BlockedKeywords = endpoint.SMTP.BlockedKeywords
			}

			smtpHandler := smtp.NewHandler(smtpClient, engine, smtpHandlerCfg)
			if approvalMgr != nil {
				smtpHandler.SetApprovalManager(approvalMgr)
			}
			h = auditMiddleware(auditLog, name, smtpHandler)
			log.Printf("Registered SMTP endpoint: /%s/ -> %s:%d", name, smtpConnCfg.Host, smtpConnCfg.Port)

		case "exec":
			execHandler := execpkg.NewHandler(engine, name)
			if approvalMgr != nil {
				execHandler.SetApprovalManager(approvalMgr)
			}
			h = auditMiddleware(auditLog, name, execHandler)
			log.Printf("Registered exec endpoint: /%s/", name)

		default: // "http" or unspecified
			p := proxy.NewWithName(name, endpoint, vault, engine)
			if approvalMgr != nil {
				p.SetApprovalManager(approvalMgr)
			}
			h = auditMiddleware(auditLog, name, p)
			log.Printf("Registered HTTP endpoint: /%s/ -> %s", name, endpoint.Upstream)
		}

		apiMux.Handle("/"+name+"/", http.StripPrefix("/"+name, h))
	}

	// Build endpoint info for discovery API
	endpointInfos := make([]discovery.EndpointInfo, 0, len(cfg.Endpoints))
	for name, endpoint := range cfg.Endpoints {
		endpointInfos = append(endpointInfos, discovery.EndpointInfo{
			Name:        name,
			Description: cfg.GetEndpointDescription(name, endpoint),
			Upstream:    endpoint.Upstream,
			DocsURL:     endpoint.DocsURL,
		})
	}

	// Register discovery API
	discoveryHandler := discovery.NewHandler(endpointInfos)
	apiMux.Handle("/endpoints", discoveryHandler)

	// Wrap API endpoints with agent authentication
	authedAPI := auth.NewAgentAuthMiddleware(cfg.Agents, apiMux)

	// Create root mux
	rootMux := http.NewServeMux()

	// Register admin UI if admin key is configured
	if cfg.Server.AdminKeyEnv != "" {
		adminKey := os.Getenv(cfg.Server.AdminKeyEnv)
		if adminKey != "" {
			if approvalMgr == nil {
				// Create approval manager even without notifiers for admin UI
				timeout := 5 * time.Minute
				if cfg.Notify.Timeout != "" {
					if d, err := time.ParseDuration(cfg.Notify.Timeout); err == nil {
						timeout = d
					}
				}
				baseURL := cfg.Server.BaseURL
				if baseURL == "" {
					baseURL = "http://localhost" + cfg.Server.Listen
				}
				approvalMgr = approval.NewManager(baseURL, timeout)
			}
			adminHandler := approval.NewAdminHandler(approvalMgr, adminKey)
			adminHandler.SetLogStore(logStore)
			uiHandler := approval.NewUIHandler(adminHandler)
			rootMux.Handle("/ui/", uiHandler)
			log.Printf("Admin UI enabled at /ui/")
		} else {
			log.Printf("Warning: admin_key_env %s is set but empty, admin UI disabled", cfg.Server.AdminKeyEnv)
		}
	}

	// Register conclave hub if conclaves are configured
	if cfg.Conclaves != nil && len(cfg.Conclaves) > 0 {
		conclaveConfigs := make(map[string]hub.ConclaveConfig, len(cfg.Conclaves))
		for name, cc := range cfg.Conclaves {
			conclaveConfigs[name] = hub.ConclaveConfig{
				Name:   name,
				KeyEnv: cc.KeyEnv,
			}
		}
		conclaveHub := hub.NewHub(version, conclaveConfigs)
		rootMux.Handle("/conclaves/ws", conclaveHub)

		// Conclave exec handler (behind agent auth)
		conclaveExecHandler := hub.NewExecHandler(conclaveHub, cfg.Conclaves)
		if approvalMgr != nil {
			conclaveExecHandler.SetApprovalManager(approvalMgr)
		}
		apiMux.Handle("/conclaves/", http.StripPrefix("/conclaves", auditMiddleware(auditLog, "conclaves", conclaveExecHandler)))

		log.Printf("Conclave hub enabled (%d conclaves configured)", len(cfg.Conclaves))
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

// parseIMAPUpstream parses IMAP connection config from endpoint settings.
// Upstream format: imaps://imap.example.com:993 or imap://imap.example.com:143
func parseIMAPUpstream(endpoint config.Endpoint, vault auth.Vault) (imap.ConnectionConfig, error) {
	u, err := url.Parse(endpoint.Upstream)
	if err != nil {
		return imap.ConnectionConfig{}, err
	}

	host := u.Hostname()
	port := 993 // Default IMAPS port
	tls := true

	if u.Scheme == "imap" {
		port = 143
		tls = false
	}

	if u.Port() != "" {
		if p, err := strconv.Atoi(u.Port()); err == nil {
			port = p
		}
	}

	// Override TLS settings from config if specified
	insecureSkipVerify := false
	if endpoint.IMAP != nil {
		tls = endpoint.IMAP.TLS
		insecureSkipVerify = endpoint.IMAP.InsecureSkipVerify
	}

	// Get credentials
	cred, err := vault.Get(endpoint.Auth.CredentialEnv)
	if err != nil {
		return imap.ConnectionConfig{}, err
	}

	// Parse username:password from credential
	username := ""
	password := cred
	if idx := strings.Index(cred, ":"); idx > 0 {
		username = cred[:idx]
		password = cred[idx+1:]
	}

	return imap.ConnectionConfig{
		Host:               host,
		Port:               port,
		Username:           username,
		Password:           password,
		TLS:                tls,
		InsecureSkipVerify: insecureSkipVerify,
	}, nil
}

// parseSMTPUpstream parses SMTP connection config from endpoint settings.
// Upstream format: smtps://smtp.example.com:465 or smtp://smtp.example.com:587
func parseSMTPUpstream(endpoint config.Endpoint, vault auth.Vault) (smtp.ConnectionConfig, error) {
	u, err := url.Parse(endpoint.Upstream)
	if err != nil {
		return smtp.ConnectionConfig{}, err
	}

	host := u.Hostname()
	port := 587 // Default SMTP submission port
	useTLS := false
	useStartTLS := true

	if u.Scheme == "smtps" {
		port = 465
		useTLS = true
		useStartTLS = false
	}

	if u.Port() != "" {
		if p, err := strconv.Atoi(u.Port()); err == nil {
			port = p
		}
	}

	// Override TLS settings from config if specified
	from := ""
	insecureSkipVerify := false
	if endpoint.SMTP != nil {
		if endpoint.SMTP.TLS {
			useTLS = true
			useStartTLS = false
		}
		if endpoint.SMTP.StartTLS {
			useTLS = false
			useStartTLS = true
		}
		from = endpoint.SMTP.From
		insecureSkipVerify = endpoint.SMTP.InsecureSkipVerify
	}

	// Get credentials
	cred, err := vault.Get(endpoint.Auth.CredentialEnv)
	if err != nil {
		return smtp.ConnectionConfig{}, err
	}

	// Parse username:password from credential
	username := ""
	password := cred
	if idx := strings.Index(cred, ":"); idx > 0 {
		username = cred[:idx]
		password = cred[idx+1:]
	}

	return smtp.ConnectionConfig{
		Host:               host,
		Port:               port,
		Username:           username,
		Password:           password,
		TLS:                useTLS,
		StartTLS:           useStartTLS,
		InsecureSkipVerify: insecureSkipVerify,
		From:               from,
	}, nil
}
