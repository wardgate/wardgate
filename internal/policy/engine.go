package policy

import (
	"regexp"
	"strings"
	"time"

	"github.com/wardgate/wardgate/internal/config"
	"github.com/wardgate/wardgate/internal/ratelimit"
)

// Action represents a policy decision.
type Action int

const (
	Allow Action = iota
	Deny
	Ask
	Queue
	RateLimited
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
	case RateLimited:
		return "rate_limited"
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
	rules      []config.Rule
	rateLimits map[int]*ratelimit.Registry // per-rule rate limiters
}

// New creates a new policy engine with the given rules.
func New(rules []config.Rule) *Engine {
	e := &Engine{
		rules:      rules,
		rateLimits: make(map[int]*ratelimit.Registry),
	}
	// Initialize rate limiters for rules that have them
	for i, rule := range rules {
		if rule.RateLimit != nil && rule.RateLimit.Max > 0 {
			window := parseWindow(rule.RateLimit.Window)
			e.rateLimits[i] = ratelimit.NewRegistry(rule.RateLimit.Max, window)
		}
	}
	return e
}

// Evaluate checks the request against rules and returns a decision.
// The key parameter is used for rate limiting (typically agent ID or IP).
func (e *Engine) Evaluate(method, path string) Decision {
	return e.EvaluateWithKey(method, path, "default")
}

// EvaluateWithKey checks rules with a key for rate limiting.
func (e *Engine) EvaluateWithKey(method, path, key string) Decision {
	for i, rule := range e.rules {
		if e.matchRule(rule, method, path) {
			// Check time range
			if rule.TimeRange != nil && !e.inTimeRange(rule.TimeRange) {
				continue // Rule doesn't apply outside time range
			}

			// Check rate limit
			if reg, ok := e.rateLimits[i]; ok {
				if !reg.Allow(key) {
					return Decision{
						Action:  RateLimited,
						Message: "rate limit exceeded",
					}
				}
			}

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

// EvaluateExec checks exec rules and returns a decision.
// command is the resolved absolute path, args is the joined argument string, cwd is the working directory.
func (e *Engine) EvaluateExec(command, args, cwd, key string) Decision {
	for i, rule := range e.rules {
		if e.matchExecRule(rule, command, args, cwd) {
			// Check time range
			if rule.TimeRange != nil && !e.inTimeRange(rule.TimeRange) {
				continue
			}

			// Check rate limit
			if reg, ok := e.rateLimits[i]; ok {
				if !reg.Allow(key) {
					return Decision{
						Action:  RateLimited,
						Message: "rate limit exceeded",
					}
				}
			}

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

func (e *Engine) matchExecRule(rule config.Rule, command, args, cwd string) bool {
	// Check command pattern
	if rule.Match.Command != "" && rule.Match.Command != "*" {
		if !MatchGlob(rule.Match.Command, command) {
			return false
		}
	}

	// Check args pattern (regex)
	if rule.Match.ArgsPattern != "" {
		matched, err := regexp.MatchString(rule.Match.ArgsPattern, args)
		if err != nil || !matched {
			return false
		}
	}

	// Check cwd pattern
	if rule.Match.CwdPattern != "" {
		if !MatchGlob(rule.Match.CwdPattern, cwd) {
			return false
		}
	}

	return true
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
	// Handle glob patterns with * in the middle (e.g., "/tasks/*/close")
	if strings.Contains(pattern, "*") {
		return MatchGlob(pattern, path)
	}
	// Exact match
	return pattern == path
}

// MatchGlob matches a path against a glob pattern.
// Supports * as a wildcard for a single path segment.
// Supports ** or trailing * for matching multiple segments.
func MatchGlob(pattern, path string) bool {
	// Trailing wildcard: "/tasks*" or "/tasks/*"
	if strings.HasSuffix(pattern, "*") && !strings.HasSuffix(pattern, "**") {
		prefix := strings.TrimSuffix(pattern, "*")
		prefix = strings.TrimSuffix(prefix, "/")
		return strings.HasPrefix(path, prefix)
	}

	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	pi := 0 // pattern index
	for i := 0; i < len(pathParts); i++ {
		if pi >= len(patternParts) {
			return false
		}

		pp := patternParts[pi]
		if pp == "**" {
			// ** matches zero or more segments
			if pi == len(patternParts)-1 {
				return true // ** at end matches everything
			}
			// Try to match remaining pattern
			for j := i; j <= len(pathParts); j++ {
				if MatchGlob(strings.Join(patternParts[pi+1:], "/"), strings.Join(pathParts[j:], "/")) {
					return true
				}
			}
			return false
		} else if pp == "*" {
			// * matches exactly one segment
			pi++
			continue
		} else if pp != pathParts[i] {
			return false
		}
		pi++
	}

	return pi == len(patternParts)
}

func (e *Engine) inTimeRange(tr *config.TimeRange) bool {
	now := time.Now()

	// Check days
	if len(tr.Days) > 0 {
		dayName := strings.ToLower(now.Weekday().String()[:3])
		found := false
		for _, d := range tr.Days {
			if strings.ToLower(d) == dayName {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	// Check hours
	if len(tr.Hours) > 0 {
		currentMinutes := now.Hour()*60 + now.Minute()
		inRange := false
		for _, h := range tr.Hours {
			start, end, ok := parseHourRange(h)
			if ok && currentMinutes >= start && currentMinutes <= end {
				inRange = true
				break
			}
		}
		if !inRange {
			return false
		}
	}

	return true
}

func parseHourRange(s string) (start, end int, ok bool) {
	parts := strings.Split(s, "-")
	if len(parts) != 2 {
		return 0, 0, false
	}
	start = parseTimeOfDay(parts[0])
	end = parseTimeOfDay(parts[1])
	if start < 0 || end < 0 {
		return 0, 0, false
	}
	return start, end, true
}

func parseTimeOfDay(s string) int {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return -1
	}
	var h, m int
	if _, err := time.Parse("15", parts[0]); err != nil {
		return -1
	}
	h = int(parts[0][0]-'0')*10 + int(parts[0][1]-'0')
	if len(parts[0]) == 1 {
		h = int(parts[0][0] - '0')
	}
	if _, err := time.Parse("04", parts[1]); err != nil {
		return -1
	}
	m = int(parts[1][0]-'0')*10 + int(parts[1][1]-'0')
	return h*60 + m
}

func parseWindow(s string) time.Duration {
	if s == "" {
		return time.Minute // default 1 minute
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return time.Minute
	}
	return d
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
