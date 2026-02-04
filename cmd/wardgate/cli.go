package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"
)

// CLIConfig holds CLI configuration.
type CLIConfig struct {
	BaseURL  string
	AdminKey string
}

// CLIClient is the client for CLI operations.
type CLIClient struct {
	config CLIConfig
	client *http.Client
}

// NewCLIClient creates a new CLI client.
func NewCLIClient(config CLIConfig) *CLIClient {
	return &CLIClient{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// ApprovalListItem represents an approval in the list response.
type ApprovalListItem struct {
	ID          string            `json:"id"`
	Endpoint    string            `json:"endpoint"`
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	AgentID     string            `json:"agent_id,omitempty"`
	Status      string            `json:"status"`
	CreatedAt   time.Time         `json:"created_at"`
	ExpiresAt   time.Time         `json:"expires_at"`
	ContentType string            `json:"content_type,omitempty"`
	Summary     string            `json:"summary,omitempty"`
	Body        string            `json:"body,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

func (c *CLIClient) request(method, path string) ([]byte, error) {
	url := strings.TrimRight(c.config.BaseURL, "/") + path
	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.config.AdminKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("server error (%d): %s", resp.StatusCode, string(body))
	}

	return body, nil
}

// ListApprovals returns pending approvals.
func (c *CLIClient) ListApprovals() ([]ApprovalListItem, error) {
	body, err := c.request("GET", "/ui/api/approvals")
	if err != nil {
		return nil, err
	}

	var resp struct {
		Approvals []ApprovalListItem `json:"approvals"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	return resp.Approvals, nil
}

// GetApproval returns a single approval by ID.
func (c *CLIClient) GetApproval(id string) (*ApprovalListItem, error) {
	body, err := c.request("GET", "/ui/api/approvals/"+id)
	if err != nil {
		return nil, err
	}

	var item ApprovalListItem
	if err := json.Unmarshal(body, &item); err != nil {
		return nil, err
	}

	return &item, nil
}

// Approve approves an approval request.
func (c *CLIClient) Approve(id string) error {
	_, err := c.request("POST", "/ui/api/approvals/"+id+"/approve")
	return err
}

// Deny denies an approval request.
func (c *CLIClient) Deny(id string) error {
	_, err := c.request("POST", "/ui/api/approvals/"+id+"/deny")
	return err
}

// ListHistory returns recent approval decisions.
func (c *CLIClient) ListHistory() ([]ApprovalListItem, error) {
	body, err := c.request("GET", "/ui/api/history")
	if err != nil {
		return nil, err
	}

	var resp struct {
		History []ApprovalListItem `json:"history"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}

	return resp.History, nil
}

// runCLI handles CLI commands.
func runCLI(args []string) {
	if len(args) < 2 {
		printCLIUsage()
		os.Exit(1)
	}

	// Get configuration from environment
	baseURL := os.Getenv("WARDGATE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}

	adminKey := os.Getenv("WARDGATE_ADMIN_KEY")
	if adminKey == "" {
		fmt.Fprintln(os.Stderr, "Error: WARDGATE_ADMIN_KEY environment variable is required")
		os.Exit(1)
	}

	client := NewCLIClient(CLIConfig{
		BaseURL:  baseURL,
		AdminKey: adminKey,
	})

	subcommand := args[1]
	subargs := args[2:]

	switch subcommand {
	case "list":
		cmdList(client)
	case "approve":
		if len(subargs) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: wardgate approvals approve <id>")
			os.Exit(1)
		}
		cmdApprove(client, subargs[0])
	case "deny":
		if len(subargs) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: wardgate approvals deny <id>")
			os.Exit(1)
		}
		cmdDeny(client, subargs[0])
	case "history":
		cmdHistory(client)
	case "monitor":
		cmdMonitor(client)
	case "view":
		if len(subargs) < 1 {
			fmt.Fprintln(os.Stderr, "Usage: wardgate approvals view <id>")
			os.Exit(1)
		}
		cmdView(client, subargs[0])
	default:
		printCLIUsage()
		os.Exit(1)
	}
}

func printCLIUsage() {
	fmt.Println(`Wardgate CLI - Approval Management

Usage: wardgate approvals <command> [args]

Commands:
  list              List pending approvals
  approve <id>      Approve a request by ID
  deny <id>         Deny a request by ID
  view <id>         View details of an approval
  history           Show recent approval decisions
  monitor           Watch mode - live updates with interactive approve/deny

Environment Variables:
  WARDGATE_URL          Wardgate server URL (default: http://localhost:8080)
  WARDGATE_ADMIN_KEY    Admin key for authentication (required)`)
}

func cmdList(client *CLIClient) {
	approvals, err := client.ListApprovals()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(approvals) == 0 {
		fmt.Println("No pending approvals")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tENDPOINT\tMETHOD\tPATH\tAGENT\tEXPIRES")
	for _, a := range approvals {
		expiresIn := time.Until(a.ExpiresAt).Round(time.Second)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			a.ID, a.Endpoint, a.Method, truncate(a.Path, 30), a.AgentID, expiresIn)
	}
	w.Flush()
}

func cmdApprove(client *CLIClient, id string) {
	if err := client.Approve(id); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Approved request %s\n", id)
}

func cmdDeny(client *CLIClient, id string) {
	if err := client.Deny(id); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✗ Denied request %s\n", id)
}

func cmdHistory(client *CLIClient) {
	history, err := client.ListHistory()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(history) == 0 {
		fmt.Println("No history")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tENDPOINT\tMETHOD\tPATH\tSTATUS\tTIME")
	for _, a := range history {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			a.ID, a.Endpoint, a.Method, truncate(a.Path, 30), a.Status, a.CreatedAt.Format(time.RFC3339))
	}
	w.Flush()
}

func cmdView(client *CLIClient, id string) {
	approval, err := client.GetApproval(id)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("ID:          %s\n", approval.ID)
	fmt.Printf("Endpoint:    %s\n", approval.Endpoint)
	fmt.Printf("Method:      %s\n", approval.Method)
	fmt.Printf("Path:        %s\n", approval.Path)
	fmt.Printf("Agent:       %s\n", approval.AgentID)
	fmt.Printf("Status:      %s\n", approval.Status)
	fmt.Printf("Created:     %s\n", approval.CreatedAt.Format(time.RFC3339))
	fmt.Printf("Expires:     %s\n", approval.ExpiresAt.Format(time.RFC3339))

	if approval.ContentType != "" {
		fmt.Printf("Type:        %s\n", approval.ContentType)
	}
	if approval.Summary != "" {
		fmt.Printf("Summary:     %s\n", approval.Summary)
	}
	if approval.Body != "" {
		fmt.Printf("\n--- Request Body ---\n")
		// Try to pretty-print JSON
		var prettyBody interface{}
		if err := json.Unmarshal([]byte(approval.Body), &prettyBody); err == nil {
			pretty, _ := json.MarshalIndent(prettyBody, "", "  ")
			fmt.Println(string(pretty))
		} else {
			fmt.Println(approval.Body)
		}
	}
}

func cmdMonitor(client *CLIClient) {
	fmt.Println("Monitoring approvals (press Ctrl+C to exit)...")
	fmt.Println("Commands: [a]pprove <id> | [d]eny <id> | [v]iew <id> | [r]efresh | [q]uit")
	fmt.Println()

	// Initial display
	printPendingApprovals(client)

	// Create a ticker for auto-refresh
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// Read user input in a goroutine
	inputCh := make(chan string)
	go func() {
		var input string
		for {
			fmt.Print("> ")
			fmt.Scanln(&input)
			inputCh <- input
		}
	}()

	for {
		select {
		case <-ticker.C:
			// Auto-refresh
			fmt.Print("\033[2J\033[H") // Clear screen
			fmt.Println("Monitoring approvals (press Ctrl+C to exit)...")
			fmt.Println("Commands: [a]pprove <id> | [d]eny <id> | [v]iew <id> | [r]efresh | [q]uit")
			fmt.Println()
			printPendingApprovals(client)

		case input := <-inputCh:
			parts := strings.Fields(input)
			if len(parts) == 0 {
				continue
			}

			cmd := strings.ToLower(parts[0])
			switch cmd {
			case "q", "quit":
				fmt.Println("Exiting monitor mode")
				return
			case "r", "refresh":
				printPendingApprovals(client)
			case "a", "approve":
				if len(parts) < 2 {
					fmt.Println("Usage: a <id>")
					continue
				}
				if err := client.Approve(parts[1]); err != nil {
					fmt.Printf("Error: %v\n", err)
				} else {
					fmt.Printf("✓ Approved %s\n", parts[1])
				}
				printPendingApprovals(client)
			case "d", "deny":
				if len(parts) < 2 {
					fmt.Println("Usage: d <id>")
					continue
				}
				if err := client.Deny(parts[1]); err != nil {
					fmt.Printf("Error: %v\n", err)
				} else {
					fmt.Printf("✗ Denied %s\n", parts[1])
				}
				printPendingApprovals(client)
			case "v", "view":
				if len(parts) < 2 {
					fmt.Println("Usage: v <id>")
					continue
				}
				cmdView(client, parts[1])
			default:
				fmt.Println("Unknown command. Use: a <id>, d <id>, v <id>, r, q")
			}
		}
	}
}

func printPendingApprovals(client *CLIClient) {
	approvals, err := client.ListApprovals()
	if err != nil {
		fmt.Printf("Error fetching approvals: %v\n", err)
		return
	}

	if len(approvals) == 0 {
		fmt.Println("No pending approvals")
		return
	}

	fmt.Printf("Pending Approvals (%d):\n", len(approvals))
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tENDPOINT\tMETHOD\tSUMMARY/PATH\tEXPIRES")
	for _, a := range approvals {
		expiresIn := time.Until(a.ExpiresAt).Round(time.Second)
		display := a.Path
		if a.Summary != "" {
			display = a.Summary
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			a.ID, a.Endpoint, a.Method, truncate(display, 40), expiresIn)
	}
	w.Flush()
	fmt.Println()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}
