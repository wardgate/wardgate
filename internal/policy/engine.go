package policy

import (
	"strings"

	"github.com/wardgate/wardgate/internal/config"
)

// Action represents a policy decision.
type Action int

const (
	Allow Action = iota
	Deny
	Ask
	Queue
)

func (a Action) String() string {
	switch a {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	case Ask:
		return "ask"
	case Queue:
		return "queue"
	default:
		return "unknown"
	}
}

// Decision is the result of policy evaluation.
type Decision struct {
	Action  Action
	Message string
}

// Engine evaluates policy rules.
type Engine struct {
	rules []config.Rule
}

// New creates a new policy engine with the given rules.
func New(rules []config.Rule) *Engine {
	return &Engine{rules: rules}
}

// Evaluate checks the request against rules and returns a decision.
func (e *Engine) Evaluate(method, path string) Decision {
	for _, rule := range e.rules {
		if e.matchRule(rule, method, path) {
			return Decision{
				Action:  parseAction(rule.Action),
				Message: rule.Message,
			}
		}
	}
	// Default deny if no rules match
	return Decision{
		Action:  Deny,
		Message: "no matching rule - default deny",
	}
}

func (e *Engine) matchRule(rule config.Rule, method, path string) bool {
	// Check method
	if rule.Match.Method != "" && rule.Match.Method != "*" {
		if rule.Match.Method != method {
			return false
		}
	}

	// Check path
	if rule.Match.Path != "" {
		if !matchPath(rule.Match.Path, path) {
			return false
		}
	}

	return true
}

func matchPath(pattern, path string) bool {
	// Wildcard suffix match (e.g., "/tasks*" matches "/tasks", "/tasks/123")
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(path, prefix)
	}
	// Exact match
	return pattern == path
}

func parseAction(action string) Action {
	switch action {
	case "allow":
		return Allow
	case "deny":
		return Deny
	case "ask":
		return Ask
	case "queue":
		return Queue
	default:
		return Deny
	}
}
