package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/wardgate/wardgate/internal/audit"
	"github.com/wardgate/wardgate/internal/auth"
	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/policy"
	"github.com/wardgate/wardgate/internal/proxy"
)

func main() {
	configPath := flag.String("config", "config.yaml", "path to config file")
	envPath := flag.String("env", ".env", "path to .env file (ignored if not found)")
	flag.Parse()

	// Load .env file (silently ignore if not found)
	if err := godotenv.Load(*envPath); err == nil {
		fmt.Printf("Loaded environment from %s\n", *envPath)
	}

	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	vault := auth.NewEnvVault()
	auditLog := audit.New(os.Stdout)

	// Create router
	mux := http.NewServeMux()

	// Register each endpoint
	for name, endpoint := range cfg.Endpoints {
		engine := policy.New(endpoint.Rules)
		p := proxy.New(endpoint, vault, engine)

		// Wrap with audit logging
		handler := auditMiddleware(auditLog, name, p)
		mux.Handle("/"+name+"/", http.StripPrefix("/"+name, handler))
	}

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Wrap with agent auth if agents are configured
	var handler http.Handler = mux
	if len(cfg.Agents) > 0 {
		handler = auth.NewAgentAuthMiddleware(cfg.Agents, mux)
	}

	server := &http.Server{
		Addr:         cfg.Server.Listen,
		Handler:      handler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, os.Interrupt, syscall.SIGTERM)

	go func() {
		fmt.Printf("Wardgate listening on %s\n", cfg.Server.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			os.Exit(1)
		}
	}()

	<-done
	fmt.Println("\nShutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := server.Shutdown(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "shutdown error: %v\n", err)
	}
	fmt.Println("Shutdown complete")
}

func auditMiddleware(log *audit.Logger, endpoint string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		requestID := uuid.New().String()

		// Wrap response writer to capture status
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(rw, r)

		log.Log(audit.Entry{
			RequestID:      requestID,
			Endpoint:       endpoint,
			Method:         r.Method,
			Path:           r.URL.Path,
			SourceIP:       strings.Split(r.RemoteAddr, ":")[0],
			Decision:       decisionFromStatus(rw.status),
			UpstreamStatus: rw.status,
			ResponseBytes:  int64(rw.bytes),
			DurationMs:     time.Since(start).Milliseconds(),
		})
	})
}

type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	n, err := rw.ResponseWriter.Write(b)
	rw.bytes += n
	return n, err
}

func decisionFromStatus(status int) string {
	if status == http.StatusForbidden {
		return "deny"
	}
	return "allow"
}
