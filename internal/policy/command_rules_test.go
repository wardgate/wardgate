package policy

import (
	"testing"

	"github.com/wardgate/wardgate/internal/config"
)

func TestEvaluateCommandRules_BasicGlob(t *testing.T) {
	rules := []config.CommandRule{
		{Match: map[string]string{"file": "notes/**"}, Action: "allow"},
		{Match: map[string]string{"file": "config/**"}, Action: "ask"},
	}

	tests := []struct {
		file   string
		expect Action
	}{
		{"notes/todo.md", Allow},
		{"notes/2024/jan.md", Allow},
		{"config/settings.yaml", Ask},
		{"private/secrets.txt", Deny}, // no match -> default deny
	}

	for _, tt := range tests {
		args := map[string]string{"file": tt.file}
		decision := EvaluateCommandRules(rules, args)
		if decision.Action != tt.expect {
			t.Errorf("file %q: expected %v, got %v", tt.file, tt.expect, decision.Action)
		}
	}
}

func TestEvaluateCommandRules_FirstMatchWins(t *testing.T) {
	rules := []config.CommandRule{
		{Match: map[string]string{"file": "notes/private/**"}, Action: "deny"},
		{Match: map[string]string{"file": "notes/**"}, Action: "allow"},
	}

	tests := []struct {
		file   string
		expect Action
	}{
		{"notes/private/diary.md", Deny}, // first rule wins
		{"notes/todo.md", Allow},         // second rule
		{"other/file.txt", Deny},         // no match -> default deny
	}

	for _, tt := range tests {
		args := map[string]string{"file": tt.file}
		decision := EvaluateCommandRules(rules, args)
		if decision.Action != tt.expect {
			t.Errorf("file %q: expected %v, got %v", tt.file, tt.expect, decision.Action)
		}
	}
}

func TestEvaluateCommandRules_MultipleMatchFields(t *testing.T) {
	rules := []config.CommandRule{
		{
			Match:  map[string]string{"file": "notes/**", "old_text": "TODO*"},
			Action: "allow",
		},
		{
			Match:  map[string]string{"file": "notes/**"},
			Action: "ask",
		},
	}

	tests := []struct {
		name   string
		args   map[string]string
		expect Action
	}{
		{
			"both match",
			map[string]string{"file": "notes/todo.md", "old_text": "TODO: fix bug"},
			Allow,
		},
		{
			"file matches but old_text doesn't",
			map[string]string{"file": "notes/todo.md", "old_text": "some other text"},
			Ask,
		},
		{
			"neither matches",
			map[string]string{"file": "config/app.yaml", "old_text": "TODO"},
			Deny,
		},
	}

	for _, tt := range tests {
		decision := EvaluateCommandRules(rules, tt.args)
		if decision.Action != tt.expect {
			t.Errorf("%s: expected %v, got %v", tt.name, tt.expect, decision.Action)
		}
	}
}

func TestEvaluateCommandRules_Wildcard(t *testing.T) {
	rules := []config.CommandRule{
		{Match: map[string]string{"file": "*"}, Action: "allow"},
	}

	args := map[string]string{"file": "anything/at/all.txt"}
	decision := EvaluateCommandRules(rules, args)
	if decision.Action != Allow {
		t.Errorf("expected Allow for wildcard, got %v", decision.Action)
	}
}

func TestEvaluateCommandRules_DoubleStarWildcard(t *testing.T) {
	rules := []config.CommandRule{
		{Match: map[string]string{"file": "**"}, Action: "allow"},
	}

	args := map[string]string{"file": "deeply/nested/path/file.txt"}
	decision := EvaluateCommandRules(rules, args)
	if decision.Action != Allow {
		t.Errorf("expected Allow for ** wildcard, got %v", decision.Action)
	}
}

func TestEvaluateCommandRules_NoRulesDefaultDeny(t *testing.T) {
	decision := EvaluateCommandRules(nil, map[string]string{"file": "test.txt"})
	if decision.Action != Deny {
		t.Errorf("expected Deny for nil rules, got %v", decision.Action)
	}

	decision = EvaluateCommandRules([]config.CommandRule{}, map[string]string{"file": "test.txt"})
	if decision.Action != Deny {
		t.Errorf("expected Deny for empty rules, got %v", decision.Action)
	}
}

func TestEvaluateCommandRules_MissingArgDenies(t *testing.T) {
	rules := []config.CommandRule{
		{Match: map[string]string{"file": "notes/**"}, Action: "allow"},
	}

	// Arg "file" not present in values
	args := map[string]string{"other": "notes/todo.md"}
	decision := EvaluateCommandRules(rules, args)
	if decision.Action != Deny {
		t.Errorf("expected Deny when matched arg is missing, got %v", decision.Action)
	}
}

func TestEvaluateCommandRules_Message(t *testing.T) {
	rules := []config.CommandRule{
		{Match: map[string]string{"file": "private/**"}, Action: "deny", Message: "private files are off limits"},
	}

	args := map[string]string{"file": "private/secret.txt"}
	decision := EvaluateCommandRules(rules, args)
	if decision.Action != Deny {
		t.Errorf("expected Deny, got %v", decision.Action)
	}
	if decision.Message != "private files are off limits" {
		t.Errorf("expected message, got %q", decision.Message)
	}
}
