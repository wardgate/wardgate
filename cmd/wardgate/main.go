package main

import (
	"encoding/json"
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
	"github.com/wardgate/wardgate/internal/grants"
	"github.com/wardgate/wardgate/internal/hub"
	"github.com/wardgate/wardgate/internal/imap"
	"github.com/wardgate/wardgate/internal/manage"
	"github.com/wardgate/wardgate/internal/notify"
	"github.com/wardgate/wardgate/internal/policy"
	"github.com/wardgate/wardgate/internal/proxy"
	"github.com/wardgate/wardgate/internal/smtp"
	sshpkg "github.com/wardgate/wardgate/internal/ssh"
)

// Set by goreleaser ldflags
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

func printUsage() {
	fmt.Fprintf(os.Stderr, `wardgate %s - Security gateway for AI agents

Usage:
  wardgate [flags]                    Start the gateway server
  wardgate <command> [args]           Run a management command

Server flags:
  -config string   Path to config file (default "config.yaml")
  -env string      Path to .env file (default ".env")
  -version         Show version and exit

Commands:
  approvals   Manage pending approval requests (list, approve, deny, view, history, monitor)
  agent       Manage agents (list, add, remove)
  conclave    Manage conclaves (list, add, remove)
  grants      Manage dynamic grants (list, add, revoke)

Run 'wardgate <command>' with no args for command-specific help.
`, version)
}

func main() {
	// Check for subcommands before parsing flags
	if len(os.Args) > 1 {
		arg := os.Args[1]
		switch arg {
		case "help", "--help", "-h":
			printUsage()
			os.Exit(0)
		case "approvals":
			runCLI(os.Args[1:])
			return
		case "agent":
			runAgentCmd(os.Args[2:])
			return
		case "conclave":
			runConclaveCmd(os.Args[2:])
			return
		case "grants":
			runGrantsCmd(os.Args[2:])
			return
		default:
			if !strings.HasPrefix(arg, "-") {
				fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", arg)
				printUsage()
				os.Exit(1)
			}
		}
	}

	// Override default flag usage to show full help
	flag.Usage = printUsage

	configPath := flag.String("config", "config.yaml", "Path to config file")
	envPath := flag.String("env", ".env", "Path to .env file")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("wardgate %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	log.Printf("Wardgate %s (commit: %s, built: %s)", version, commit, date)

	// Load environment variables
	if err := godotenv.Load(*envPath); err != nil {
		log.Printf("Warning: could not load %s: %v", *envPath, err)
	}

	// Load configuration
	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	if err := cfg.ValidateEnv(); err != nil {
		log.Fatalf("Config environment check failed: %v", err)
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

	// Load grant store
	grantsFile := "grants.json"
	if cfg.Server.GrantsFile != "" {
		grantsFile = cfg.Server.GrantsFile
	}
	grantStore, err := grants.LoadStore(grantsFile)
	if err != nil {
		log.Printf("Warning: could not load grants from %s: %v (starting with empty grants)", grantsFile, err)
		grantStore = grants.NewStore(grantsFile)
	}
	log.Printf("Loaded %d active grants from %s", len(grantStore.List()), grantsFile)

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

		case "ssh":
			sshConnCfg, err := parseSSHConfig(endpoint, vault)
			if err != nil {
				log.Fatalf("Failed to parse SSH config for %s: %v", name, err)
			}

			sshPool := sshpkg.NewPoolWithDialer(&sshDialerAdapter{}, sshpkg.PoolConfig{
				MaxConnsPerEndpoint: sshConnCfg.MaxSessions,
				IdleTimeout:         5 * time.Minute,
			})
			sshHandler := sshpkg.NewHandler(sshPool, engine, sshpkg.HandlerConfig{
				EndpointName:     name,
				ConnectionConfig: sshConnCfg,
			})
			if approvalMgr != nil {
				sshHandler.SetApprovalManager(approvalMgr)
			}
			h = auditMiddleware(auditLog, name, sshHandler)
			log.Printf("Registered SSH endpoint: /%s/ -> %s:%d", name, sshConnCfg.Host, sshConnCfg.Port)

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
			p.SetGrantStore(grantStore)
			h = auditMiddleware(auditLog, name, p)
			log.Printf("Registered HTTP endpoint: /%s/ -> %s", name, endpoint.Upstream)
		}

		// Wrap with agent scope middleware if endpoint has agents restriction
		if len(endpoint.Agents) > 0 {
			h = agentScopeMiddleware(endpoint.Agents, h)
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
			Agents:      endpoint.Agents,
		})
	}

	// Register discovery API
	discoveryHandler := discovery.NewHandler(endpointInfos)
	apiMux.Handle("/endpoints", discoveryHandler)

	// Wrap API endpoints with agent authentication
	authedAPI := auth.NewAgentAuthMiddleware(cfg.Agents, cfg.Server.JWT, apiMux)

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
			adminHandler.SetGrantStore(grantStore)
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
		conclaveExecHandler.SetGrantStore(grantStore)
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

func agentScopeMiddleware(agents []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agentID := r.Header.Get("X-Agent-ID")
		if !auth.AgentAllowed(agents, agentID) {
			http.Error(w, "agent not allowed for this endpoint", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
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

// parseSSHConfig builds an SSH ConnectionConfig from endpoint settings.
func parseSSHConfig(endpoint config.Endpoint, vault auth.Vault) (sshpkg.ConnectionConfig, error) {
	if endpoint.SSH == nil {
		return sshpkg.ConnectionConfig{}, fmt.Errorf("ssh config is required for ssh adapter")
	}

	cfg := endpoint.SSH

	port := cfg.Port
	if port == 0 {
		port = 22
	}

	maxSessions := cfg.MaxSessions
	if maxSessions == 0 {
		maxSessions = 5
	}

	timeoutSecs := cfg.TimeoutSecs
	if timeoutSecs == 0 {
		timeoutSecs = 30
	}

	// Get SSH private key from vault
	key, err := vault.Get(endpoint.Auth.CredentialEnv)
	if err != nil {
		return sshpkg.ConnectionConfig{}, fmt.Errorf("getting SSH key: %w", err)
	}

	return sshpkg.ConnectionConfig{
		Host:               cfg.Host,
		Port:               port,
		Username:           cfg.Username,
		PrivateKey:         []byte(key),
		KnownHost:          cfg.KnownHost,
		KnownHostsFile:     cfg.KnownHostsFile,
		InsecureSkipVerify: cfg.InsecureSkipVerify,
		MaxSessions:        maxSessions,
		TimeoutSecs:        timeoutSecs,
	}, nil
}

// sshDialerAdapter adapts the SSH client constructor to the Dialer interface.
type sshDialerAdapter struct{}

func (d *sshDialerAdapter) Dial(cfg sshpkg.ConnectionConfig) (sshpkg.Client, error) {
	return sshpkg.NewSSHClient(cfg)
}

func runAgentCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: wardgate agent <list|add|remove> [options]\n")
		os.Exit(1)
	}

	configPath := "config.yaml"
	envPath := ".env"

	switch args[0] {
	case "list":
		agents, err := manage.ListAgents(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(agents) == 0 {
			fmt.Println("No agents configured.")
			return
		}
		for _, a := range agents {
			fmt.Printf("%-20s key_env=%s\n", a.ID, a.KeyEnv)
		}

	case "add":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: wardgate agent add <id>\n")
			os.Exit(1)
		}
		id := args[1]
		keyEnvName := "WARDGATE_AGENT_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_KEY"

		key, err := manage.GenerateKey()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating key: %v\n", err)
			os.Exit(1)
		}
		if err := manage.AppendEnvVar(envPath, keyEnvName, key); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := manage.AddAgent(configPath, id, keyEnvName); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Agent %q added.\n", id)
		fmt.Printf("Key:     %s\n", key)
		fmt.Printf("Env var: %s\n", keyEnvName)
		fmt.Printf("\nConfigure wardgate-cli with this key.\n")

	case "remove":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: wardgate agent remove <id>\n")
			os.Exit(1)
		}
		id := args[1]
		keyEnvName := "WARDGATE_AGENT_" + strings.ToUpper(strings.ReplaceAll(id, "-", "_")) + "_KEY"

		if err := manage.RemoveAgent(configPath, id); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		_ = manage.RemoveEnvVar(envPath, keyEnvName) // best effort
		fmt.Printf("Agent %q removed.\n", id)

	default:
		fmt.Fprintf(os.Stderr, "Unknown agent subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func runConclaveCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: wardgate conclave <list|add|remove> [options]\n")
		os.Exit(1)
	}

	configPath := "config.yaml"
	envPath := ".env"

	switch args[0] {
	case "list":
		conclaves, err := manage.ListConclaves(configPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if len(conclaves) == 0 {
			fmt.Println("No conclaves configured.")
			return
		}
		for _, c := range conclaves {
			desc := c.Description
			if desc == "" {
				desc = "(no description)"
			}
			fmt.Printf("%-20s %s\n", c.Name, desc)
		}

	case "add":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: wardgate conclave add <name> [description]\n")
			os.Exit(1)
		}
		name := args[1]
		description := ""
		if len(args) > 2 {
			description = args[2]
		}
		keyEnvName := "WARDGATE_CONCLAVE_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_KEY"

		key, err := manage.GenerateKey()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error generating key: %v\n", err)
			os.Exit(1)
		}
		if err := manage.AppendEnvVar(envPath, keyEnvName, key); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		if err := manage.AddConclave(configPath, name, keyEnvName, description); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Conclave %q added.\n", name)
		fmt.Printf("Key:     %s\n", key)
		fmt.Printf("Env var: %s\n", keyEnvName)
		fmt.Printf("\nwardgate-exec config:\n")
		fmt.Printf("  server: wss://your-wardgate-host/conclaves/ws\n")
		fmt.Printf("  key: %s\n", key)
		fmt.Printf("  name: %s\n", name)

	case "remove":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: wardgate conclave remove <name>\n")
			os.Exit(1)
		}
		name := args[1]
		keyEnvName := "WARDGATE_CONCLAVE_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")) + "_KEY"

		if err := manage.RemoveConclave(configPath, name); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		_ = manage.RemoveEnvVar(envPath, keyEnvName) // best effort
		fmt.Printf("Conclave %q removed.\n", name)

	default:
		fmt.Fprintf(os.Stderr, "Unknown conclave subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func loadAdminClient() *CLIClient {
	_ = godotenv.Load(".env") // best effort

	baseURL := os.Getenv("WARDGATE_URL")
	adminKey := os.Getenv("WARDGATE_ADMIN_KEY")

	// Try reading config.yaml for defaults
	if baseURL == "" || adminKey == "" {
		if cfg, err := config.LoadFromFile("config.yaml"); err == nil {
			if baseURL == "" {
				if cfg.Server.BaseURL != "" {
					baseURL = cfg.Server.BaseURL
				} else if cfg.Server.Listen != "" {
					baseURL = "http://localhost" + cfg.Server.Listen
				}
			}
			if adminKey == "" && cfg.Server.AdminKeyEnv != "" {
				adminKey = os.Getenv(cfg.Server.AdminKeyEnv)
			}
		}
	}

	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	if adminKey == "" {
		fmt.Fprintln(os.Stderr, "Error: WARDGATE_ADMIN_KEY not found (set env var or configure admin_key_env in config.yaml)")
		os.Exit(1)
	}
	return NewCLIClient(CLIConfig{BaseURL: baseURL, AdminKey: adminKey})
}

func runGrantsCmd(args []string) {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: wardgate grants <list|add|revoke> [options]\n")
		os.Exit(1)
	}

	client := loadAdminClient()

	switch args[0] {
	case "list":
		body, err := client.request("GET", "/ui/api/grants")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		var resp struct {
			Grants []grants.Grant `json:"grants"`
		}
		if err := json.Unmarshal(body, &resp); err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing response: %v\n", err)
			os.Exit(1)
		}
		if len(resp.Grants) == 0 {
			fmt.Println("No active grants.")
			return
		}
		for _, g := range resp.Grants {
			expiry := "permanent"
			if g.ExpiresAt != nil {
				expiry = fmt.Sprintf("expires %s", g.ExpiresAt.Format(time.RFC3339))
			}
			fmt.Printf("%-20s agent=%-10s scope=%-25s action=%-6s %s\n", g.ID, g.AgentID, g.Scope, g.Action, expiry)
			if g.Match.Command != "" {
				fmt.Printf("  command=%s\n", g.Match.Command)
			}
			if g.Match.Method != "" {
				fmt.Printf("  method=%s path=%s\n", g.Match.Method, g.Match.Path)
			}
		}

	case "add":
		if len(args) < 4 {
			fmt.Fprintf(os.Stderr, "Usage: wardgate grants add <agent> <scope> <match> [--duration <dur>]\n")
			fmt.Fprintf(os.Stderr, "  Example: wardgate grants add tessa conclave:obsidian command:rg\n")
			fmt.Fprintf(os.Stderr, "  Example: wardgate grants add tessa endpoint:todoist method:DELETE,path:/tasks/*\n")
			os.Exit(1)
		}

		g := grants.Grant{
			AgentID: args[1],
			Scope:   args[2],
			Action:  "allow",
			Reason:  "added via CLI",
		}

		// Parse match
		matchStr := args[3]
		for _, part := range strings.Split(matchStr, ",") {
			kv := strings.SplitN(part, ":", 2)
			if len(kv) != 2 {
				continue
			}
			switch kv[0] {
			case "command":
				g.Match.Command = kv[1]
			case "method":
				g.Match.Method = kv[1]
			case "path":
				g.Match.Path = kv[1]
			}
		}

		// Parse optional duration
		for i := 4; i < len(args)-1; i++ {
			if args[i] == "--duration" {
				d, err := time.ParseDuration(args[i+1])
				if err != nil {
					fmt.Fprintf(os.Stderr, "Invalid duration: %v\n", err)
					os.Exit(1)
				}
				exp := time.Now().Add(d)
				g.ExpiresAt = &exp
			}
		}

		payload, _ := json.Marshal(g)
		body, err := client.requestBody("POST", "/ui/api/grants", payload)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		var added grants.Grant
		json.Unmarshal(body, &added)
		fmt.Printf("Grant %s added for agent %q on %s\n", added.ID, g.AgentID, g.Scope)

	case "revoke":
		if len(args) < 2 {
			fmt.Fprintf(os.Stderr, "Usage: wardgate grants revoke <grant-id>\n")
			os.Exit(1)
		}
		_, err := client.request("DELETE", "/ui/api/grants/"+args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Grant %s revoked.\n", args[1])

	default:
		fmt.Fprintf(os.Stderr, "Unknown grants subcommand: %s\n", args[0])
		os.Exit(1)
	}
}
