package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/wardgate/wardgate/internal/cli"
)

// Set by goreleaser ldflags (or at build: -ldflags "-X main.configPath=/path")
var (
	version    = "dev"
	commit     = "none"
	date       = "unknown"
	configPath = "/etc/wardgate-cli/config.yaml" // fixed path; agent cannot override
)

func main() {
	// Single flag set: global + request flags (no -config; path is fixed at build)
	envPath := flag.String("env", ".env", "Path to .env file")
	showVersion := flag.Bool("version", false, "Show version and exit")
	method := flag.String("X", "GET", "HTTP method")
	methodLong := flag.String("request", "GET", "HTTP method")
	var headers headerSlice
	flag.Var(&headers, "H", "HTTP header (Key: Value)")
	flag.Var(&headers, "header", "HTTP header (Key: Value)")
	var data string
	flag.StringVar(&data, "d", "", "HTTP request body")
	flag.StringVar(&data, "data", "", "HTTP request body")
	flag.StringVar(&data, "data-raw", "", "HTTP request body")
	output := flag.String("o", "", "Write output to file")
	flag.StringVar(output, "output", "", "Write output to file")
	silent := flag.Bool("s", false, "Silent mode")
	flag.BoolVar(silent, "silent", false, "Silent mode")
	verbose := flag.Bool("v", false, "Verbose")
	flag.BoolVar(verbose, "verbose", false, "Verbose")
	followRedirects := flag.Bool("L", false, "Follow redirects (same-host only)")
	flag.BoolVar(followRedirects, "location", false, "Follow redirects (same-host only)")
	insecure := flag.Bool("k", false, "Allow insecure TLS (self-signed certs)")
	flag.BoolVar(insecure, "insecure", false, "Allow insecure TLS (self-signed certs)")
	writeOut := flag.String("w", "", "Write-out format")
	flag.StringVar(writeOut, "write-out", "", "Write-out format")
	flag.Parse()

	if *showVersion {
		fmt.Printf("wardgate-cli %s (commit: %s, built: %s)\n", version, commit, date)
		os.Exit(0)
	}

	// Check for endpoints subcommand
	if len(flag.Args()) > 0 && flag.Arg(0) == "endpoints" {
		runEndpoints(configPath, *envPath)
		return
	}

	// Default: request command (default method is GET when -X/--request not provided)
	m := *method
	if *methodLong != "GET" {
		m = *methodLong
	}
	if m == "" {
		m = "GET"
	}
	runRequest(configPath, *envPath, runRequestOpts{
		method:          m,
		headers:         headers,
		data:           data,
		output:         *output,
		silent:         *silent,
		verbose:        *verbose,
		followRedirects: *followRedirects,
		insecure:       *insecure,
		writeOut:       *writeOut,
	})
}

func runEndpoints(configPath, envPath string) {
	cfg, err := cli.Load(envPath, configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if cfg.Server == "" {
		fmt.Fprintf(os.Stderr, "Error: server not configured (set server in %s)\n", configPath)
		os.Exit(1)
	}

	key, err := cfg.GetKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	rootCAs, err := cfg.LoadRootCAs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	client, err := cli.NewClient(cfg.Server, key, cli.ClientOptions{RootCAs: rootCAs})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	resp, err := client.FetchEndpoints()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(resp); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

type runRequestOpts struct {
	method          string
	headers         headerSlice
	data           string
	output         string
	silent         bool
	verbose        bool
	followRedirects bool
	insecure       bool
	writeOut       string
}

func runRequest(configPath, envPath string, opts runRequestOpts) {
	pathOrURL := ""
	if flag.NArg() > 0 {
		pathOrURL = flag.Arg(0)
	}

	if pathOrURL == "" {
		fmt.Fprintln(os.Stderr, "Usage: wardgate-cli [options] <path-or-url>")
		fmt.Fprintln(os.Stderr, "       wardgate-cli endpoints")
		fmt.Fprintf(os.Stderr, "\nConfig: %s (fixed at build)\n", configPath)
		flag.PrintDefaults()
		os.Exit(1)
	}

	cfg, err := cli.Load(envPath, configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if cfg.Server == "" {
		fmt.Fprintf(os.Stderr, "Error: server not configured (set server in %s)\n", configPath)
		os.Exit(1)
	}

	key, err := cfg.GetKey()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	rootCAs, err := cfg.LoadRootCAs()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	client, err := cli.NewClient(cfg.Server, key, cli.ClientOptions{
		FollowRedirects:    opts.followRedirects,
		InsecureSkipVerify: opts.insecure,
		RootCAs:           rootCAs,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	req, err := client.BuildRequest(pathOrURL, cli.RequestOptions{
		Method:   opts.method,
		Headers:  opts.headers,
		Data:     opts.data,
		Output:   opts.output,
		Silent:   opts.silent,
		Verbose:  opts.verbose,
		WriteOut: opts.writeOut,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	// Write body to output
	var w io.Writer = os.Stdout
	if opts.output != "" {
		f, err := os.Create(opts.output)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		w = f
	}

	if !opts.silent && opts.output == "" {
		fmt.Fprintf(os.Stderr, "HTTP/%d %s\n", resp.ProtoMajor, resp.Status)
	}

	if _, err := io.Copy(w, resp.Body); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	// Handle -w write-out (appended after body, like curl)
	if opts.writeOut != "" {
		format := strings.ReplaceAll(opts.writeOut, "%{http_code}", fmt.Sprintf("%d", resp.StatusCode))
		format = strings.ReplaceAll(format, "%{http_status}", resp.Status)
		fmt.Print(format)
	}

	if resp.StatusCode >= 400 {
		os.Exit(1)
	}
}

type headerSlice []string

func (h *headerSlice) String() string { return strings.Join(*h, ", ") }
func (h *headerSlice) Set(s string) error {
	*h = append(*h, s)
	return nil
}
