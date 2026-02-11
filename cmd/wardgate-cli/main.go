package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/wardgate/wardgate/internal/cli"
	execpkg "github.com/wardgate/wardgate/internal/exec"
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

	// Check for subcommands
	if len(flag.Args()) > 0 && flag.Arg(0) == "endpoints" {
		runEndpoints(configPath, *envPath)
		return
	}
	if len(flag.Args()) > 0 && flag.Arg(0) == "conclaves" {
		runConclaves(configPath, *envPath)
		return
	}
	if len(flag.Args()) > 0 && flag.Arg(0) == "exec" {
		runExec(configPath, *envPath, flag.Args()[1:])
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

func runConclaves(configPath, envPath string) {
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

	conclavesURL, err := client.ResolveURL("/conclaves/")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	req, err := http.NewRequest(http.MethodGet, conclavesURL, nil)
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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "Error: %s\n", string(body))
		os.Exit(1)
	}

	var result json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(result)
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
		fmt.Fprintln(os.Stderr, "       wardgate-cli conclaves")
		fmt.Fprintln(os.Stderr, "       wardgate-cli exec [-C <dir>] <conclave> \"<command>\"")
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

func runExec(configPath, envPath string, args []string) {
	// Parse exec-specific flags
	execFlags := flag.NewFlagSet("exec", flag.ExitOnError)
	cwdFlag := execFlags.String("C", "", "Working directory for command execution")
	execFlags.Parse(args)

	if execFlags.NArg() < 2 {
		fmt.Fprintln(os.Stderr, "Usage: wardgate-cli exec [-C <dir>] <conclave> \"<command>\"")
		os.Exit(1)
	}

	conclaveName := execFlags.Arg(0)
	cmdStr := execFlags.Arg(1)

	// Determine cwd (optional — conclave has its own default)
	cwd := *cwdFlag
	if cwd != "" {
		var err error
		cwd, err = filepath.Abs(cwd)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: invalid working directory: %v\n", err)
			os.Exit(1)
		}
	}

	// Parse for unsafe construct detection + segment extraction
	result, err := execpkg.ParseShellCommand(cmdStr)
	if err != nil {
		if _, ok := err.(*execpkg.UnsafeShellError); ok {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			fmt.Fprintln(os.Stderr, "Commands containing $(), backticks, or subshells are not supported.")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Error parsing command: %v\n", err)
		os.Exit(1)
	}

	// Load config and create client
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

	// Build segments for policy evaluation (command names, not absolute paths)
	type execSegment struct {
		Command string `json:"command"`
		Args    string `json:"args"`
	}
	segments := make([]execSegment, len(result.Segments))
	for i, seg := range result.Segments {
		segments[i] = execSegment{
			Command: seg.Command,
			Args:    seg.Args,
		}
	}

	// Send exec request to wardgate — policy eval + execution happen server-side
	execReq := struct {
		Segments []execSegment `json:"segments"`
		Cwd      string        `json:"cwd,omitempty"`
		Raw      string        `json:"raw"`
	}{
		Segments: segments,
		Cwd:      cwd,
		Raw:      result.Raw,
	}

	body, err := json.Marshal(execReq)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	execURL, err := client.ResolveURL(fmt.Sprintf("/conclaves/%s/exec", conclaveName))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	req, err := http.NewRequest(http.MethodPost, execURL, bytes.NewReader(body))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	var execResp struct {
		Action   string `json:"action"`
		Message  string `json:"message,omitempty"`
		Stdout   string `json:"stdout,omitempty"`
		Stderr   string `json:"stderr,omitempty"`
		ExitCode *int   `json:"exit_code,omitempty"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&execResp); err != nil {
		fmt.Fprintf(os.Stderr, "Error decoding response: %v\n", err)
		os.Exit(1)
	}

	if execResp.Stderr != "" {
		fmt.Fprint(os.Stderr, execResp.Stderr)
	}
	if execResp.Stdout != "" {
		fmt.Print(execResp.Stdout)
	}

	if execResp.Action != "allow" {
		if execResp.Message != "" {
			fmt.Fprintf(os.Stderr, "Error: %s\n", execResp.Message)
		}
		os.Exit(1)
	}

	if execResp.ExitCode != nil {
		os.Exit(*execResp.ExitCode)
	}
}
