package policy

import (
	"testing"

	"github.com/wardgate/wardgate/internal/config"
)

func TestEngine_AllowRule(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Method: "GET"}, Action: "allow"},
	}
	engine := New(rules)

	decision := engine.Evaluate("GET", "/tasks")
	if decision.Action != Allow {
		t.Errorf("expected Allow, got %v", decision.Action)
	}
}

func TestEngine_DenyRule(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Method: "DELETE"}, Action: "deny", Message: "Not allowed"},
	}
	engine := New(rules)

	decision := engine.Evaluate("DELETE", "/tasks/123")
	if decision.Action != Deny {
		t.Errorf("expected Deny, got %v", decision.Action)
	}
	if decision.Message != "Not allowed" {
		t.Errorf("expected message 'Not allowed', got '%s'", decision.Message)
	}
}

func TestEngine_MethodWildcard(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Method: "*"}, Action: "deny", Message: "Catch all"},
	}
	engine := New(rules)

	for _, method := range []string{"GET", "POST", "PUT", "DELETE", "PATCH"} {
		decision := engine.Evaluate(method, "/anything")
		if decision.Action != Deny {
			t.Errorf("method %s: expected Deny, got %v", method, decision.Action)
		}
	}
}

func TestEngine_PathPattern(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Method: "GET", Path: "/tasks*"}, Action: "allow"},
		{Match: config.Match{Method: "*"}, Action: "deny"},
	}
	engine := New(rules)

	tests := []struct {
		method string
		path   string
		expect Action
	}{
		{"GET", "/tasks", Allow},
		{"GET", "/tasks/123", Allow},
		{"GET", "/tasks/123/comments", Allow},
		{"GET", "/projects", Deny},
		{"POST", "/tasks", Deny},
	}

	for _, tt := range tests {
		decision := engine.Evaluate(tt.method, tt.path)
		if decision.Action != tt.expect {
			t.Errorf("%s %s: expected %v, got %v", tt.method, tt.path, tt.expect, decision.Action)
		}
	}
}

func TestEngine_FirstMatchWins(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Method: "GET", Path: "/tasks"}, Action: "allow"},
		{Match: config.Match{Method: "GET"}, Action: "deny"},
	}
	engine := New(rules)

	// First rule should match
	decision := engine.Evaluate("GET", "/tasks")
	if decision.Action != Allow {
		t.Errorf("expected Allow (first match), got %v", decision.Action)
	}

	// Second rule should match
	decision = engine.Evaluate("GET", "/projects")
	if decision.Action != Deny {
		t.Errorf("expected Deny (second match), got %v", decision.Action)
	}
}

func TestEngine_NoMatchDefaultsDeny(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Method: "GET"}, Action: "allow"},
	}
	engine := New(rules)

	decision := engine.Evaluate("POST", "/tasks")
	if decision.Action != Deny {
		t.Errorf("expected Deny (default), got %v", decision.Action)
	}
	if decision.Message == "" {
		t.Error("expected default deny message")
	}
}

func TestEngine_EmptyRulesetDeniesAll(t *testing.T) {
	engine := New(nil)

	decision := engine.Evaluate("GET", "/anything")
	if decision.Action != Deny {
		t.Errorf("expected Deny for empty ruleset, got %v", decision.Action)
	}
}

func TestEngine_ExactPathMatch(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Method: "POST", Path: "/tasks"}, Action: "allow"},
		{Match: config.Match{Method: "*"}, Action: "deny"},
	}
	engine := New(rules)

	// Exact match
	decision := engine.Evaluate("POST", "/tasks")
	if decision.Action != Allow {
		t.Errorf("expected Allow for exact path, got %v", decision.Action)
	}

	// Not exact match (has suffix)
	decision = engine.Evaluate("POST", "/tasks/123")
	if decision.Action != Deny {
		t.Errorf("expected Deny for non-exact path, got %v", decision.Action)
	}
}

func TestEngine_GlobPatternMiddle(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Method: "POST", Path: "/tasks/*/close"}, Action: "allow"},
		{Match: config.Match{Method: "*"}, Action: "deny"},
	}
	engine := New(rules)

	tests := []struct {
		method string
		path   string
		expect Action
	}{
		{"POST", "/tasks/123/close", Allow},
		{"POST", "/tasks/abc/close", Allow},
		{"POST", "/tasks/close", Deny},         // Missing segment
		{"POST", "/tasks/123/456/close", Deny}, // Too many segments
		{"GET", "/tasks/123/close", Deny},      // Wrong method
	}

	for _, tt := range tests {
		decision := engine.Evaluate(tt.method, tt.path)
		if decision.Action != tt.expect {
			t.Errorf("%s %s: expected %v, got %v", tt.method, tt.path, tt.expect, decision.Action)
		}
	}
}

func TestEngine_GlobPatternDoubleWildcard(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Method: "GET", Path: "/api/**/status"}, Action: "allow"},
		{Match: config.Match{Method: "*"}, Action: "deny"},
	}
	engine := New(rules)

	tests := []struct {
		path   string
		expect Action
	}{
		{"/api/status", Allow},
		{"/api/v1/status", Allow},
		{"/api/v1/tasks/status", Allow},
		{"/api/v1/tasks/123/status", Allow},
		{"/api/v1/other", Deny},
	}

	for _, tt := range tests {
		decision := engine.Evaluate("GET", tt.path)
		if decision.Action != tt.expect {
			t.Errorf("GET %s: expected %v, got %v", tt.path, tt.expect, decision.Action)
		}
	}
}

func TestEngine_RateLimiting(t *testing.T) {
	rules := []config.Rule{
		{
			Match:  config.Match{Method: "GET"},
			Action: "allow",
			RateLimit: &config.RateLimit{
				Max:    3,
				Window: "1m",
			},
		},
	}
	engine := New(rules)

	// First 3 requests should be allowed
	for i := 0; i < 3; i++ {
		decision := engine.EvaluateWithKey("GET", "/tasks", "agent-1")
		if decision.Action != Allow {
			t.Errorf("request %d should be allowed, got %v", i+1, decision.Action)
		}
	}

	// 4th request should be rate limited
	decision := engine.EvaluateWithKey("GET", "/tasks", "agent-1")
	if decision.Action != RateLimited {
		t.Errorf("expected RateLimited, got %v", decision.Action)
	}

	// Different agent should have its own limit
	decision = engine.EvaluateWithKey("GET", "/tasks", "agent-2")
	if decision.Action != Allow {
		t.Errorf("different agent should be allowed, got %v", decision.Action)
	}
}

func TestEngine_RateLimitPerRule(t *testing.T) {
	rules := []config.Rule{
		{
			Match:  config.Match{Method: "POST", Path: "/tasks"},
			Action: "allow",
			RateLimit: &config.RateLimit{
				Max:    2,
				Window: "1m",
			},
		},
		{
			Match:  config.Match{Method: "GET"},
			Action: "allow",
			RateLimit: &config.RateLimit{
				Max:    10,
				Window: "1m",
			},
		},
	}
	engine := New(rules)

	// Use up POST limit
	engine.EvaluateWithKey("POST", "/tasks", "agent-1")
	engine.EvaluateWithKey("POST", "/tasks", "agent-1")
	decision := engine.EvaluateWithKey("POST", "/tasks", "agent-1")
	if decision.Action != RateLimited {
		t.Errorf("POST should be rate limited, got %v", decision.Action)
	}

	// GET should still work (different rule, different limit)
	decision = engine.EvaluateWithKey("GET", "/tasks", "agent-1")
	if decision.Action != Allow {
		t.Errorf("GET should still be allowed, got %v", decision.Action)
	}
}

// --- EvaluateExec tests ---

func TestEngine_EvaluateExec_CommandMatch(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Command: "rg"}, Action: "allow"},
		{Match: config.Match{Command: "*"}, Action: "deny"},
	}
	engine := New(rules)

	decision := engine.EvaluateExec("rg", "TODO .", "/data", "agent-1")
	if decision.Action != Allow {
		t.Errorf("expected Allow for 'rg', got %v", decision.Action)
	}

	decision = engine.EvaluateExec("rm", "-rf /", "/data", "agent-1")
	if decision.Action != Deny {
		t.Errorf("expected Deny for 'rm', got %v", decision.Action)
	}
}

func TestEngine_EvaluateExec_CommandWildcard(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Command: "*"}, Action: "allow"},
	}
	engine := New(rules)

	for _, cmd := range []string{"cat", "rg", "git", "rm", "python3"} {
		decision := engine.EvaluateExec(cmd, "", "/data", "agent-1")
		if decision.Action != Allow {
			t.Errorf("expected Allow for %q with wildcard, got %v", cmd, decision.Action)
		}
	}
}

func TestEngine_EvaluateExec_CommandGlob(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Command: "python*"}, Action: "allow"},
		{Match: config.Match{Command: "*"}, Action: "deny"},
	}
	engine := New(rules)

	tests := []struct {
		command string
		expect  Action
	}{
		{"python", Allow},
		{"python3", Allow},
		{"python3.11", Allow},
		{"ruby", Deny},
		{"node", Deny},
	}

	for _, tt := range tests {
		decision := engine.EvaluateExec(tt.command, "", "/data", "agent-1")
		if decision.Action != tt.expect {
			t.Errorf("command %q: expected %v, got %v", tt.command, tt.expect, decision.Action)
		}
	}
}

func TestEngine_EvaluateExec_ArgsPattern(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Command: "git", ArgsPattern: "^(status|log|diff)"}, Action: "allow"},
		{Match: config.Match{Command: "git"}, Action: "deny", Message: "write ops not allowed"},
	}
	engine := New(rules)

	tests := []struct {
		args   string
		expect Action
	}{
		{"status", Allow},
		{"log --oneline", Allow},
		{"diff HEAD", Allow},
		{"push origin main", Deny},
		{"commit -m fix", Deny},
		{"rebase -i HEAD~3", Deny},
	}

	for _, tt := range tests {
		decision := engine.EvaluateExec("git", tt.args, "/data", "agent-1")
		if decision.Action != tt.expect {
			t.Errorf("git %s: expected %v, got %v", tt.args, tt.expect, decision.Action)
		}
	}
}

func TestEngine_EvaluateExec_CwdPattern(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Command: "*", CwdPattern: "/data/vault/**"}, Action: "allow"},
		{Match: config.Match{Command: "*"}, Action: "deny", Message: "wrong directory"},
	}
	engine := New(rules)

	tests := []struct {
		cwd    string
		expect Action
	}{
		{"/data/vault/notes", Allow},
		{"/data/vault/notes/2024", Allow},
		{"/home/user", Deny},
		{"/data/other", Deny},
	}

	for _, tt := range tests {
		decision := engine.EvaluateExec("cat", "file.txt", tt.cwd, "agent-1")
		if decision.Action != tt.expect {
			t.Errorf("cwd %s: expected %v, got %v", tt.cwd, tt.expect, decision.Action)
		}
	}
}

func TestEngine_EvaluateExec_NoMatchDefaultDeny(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Command: "cat"}, Action: "allow"},
	}
	engine := New(rules)

	decision := engine.EvaluateExec("rm", "", "/data", "agent-1")
	if decision.Action != Deny {
		t.Errorf("expected Deny for unmatched command, got %v", decision.Action)
	}
	if decision.Message == "" {
		t.Error("expected default deny message")
	}
}

func TestEngine_EvaluateExec_FirstMatchWins(t *testing.T) {
	rules := []config.Rule{
		{Match: config.Match{Command: "git", ArgsPattern: "^status"}, Action: "allow"},
		{Match: config.Match{Command: "git"}, Action: "deny", Message: "all other git ops denied"},
	}
	engine := New(rules)

	// First rule should match
	decision := engine.EvaluateExec("git", "status", "/data", "agent-1")
	if decision.Action != Allow {
		t.Errorf("expected Allow for 'git status' (first match), got %v", decision.Action)
	}

	// Second rule should match
	decision = engine.EvaluateExec("git", "push", "/data", "agent-1")
	if decision.Action != Deny {
		t.Errorf("expected Deny for 'git push' (second match), got %v", decision.Action)
	}
}

func TestEngine_EvaluateExec_EmptyRulesetDeniesAll(t *testing.T) {
	engine := New(nil)

	decision := engine.EvaluateExec("cat", "file.txt", "/data", "agent-1")
	if decision.Action != Deny {
		t.Errorf("expected Deny for empty ruleset, got %v", decision.Action)
	}
}
