package policy

import (
	"github.com/wardgate/wardgate/internal/config"
)

// EvaluateCommandRules checks command-level rules against arg values.
// Rules are evaluated in order (first match wins). Each rule's match keys
// are arg names and values are glob patterns; all must match for the rule
// to apply (AND logic). If no rules match, returns default deny.
func EvaluateCommandRules(rules []config.CommandRule, argValues map[string]string) Decision {
	for _, rule := range rules {
		if matchCommandRule(rule, argValues) {
			return Decision{
				Action:  parseAction(rule.Action),
				Message: rule.Message,
			}
		}
	}
	return Decision{
		Action:  Deny,
		Message: "no matching command rule - default deny",
	}
}

// matchCommandRule checks if all match entries in a rule match the arg values.
func matchCommandRule(rule config.CommandRule, argValues map[string]string) bool {
	for argName, pattern := range rule.Match {
		value, ok := argValues[argName]
		if !ok {
			return false
		}
		if pattern != "*" && pattern != "**" {
			if !MatchGlob(pattern, value) {
				return false
			}
		}
	}
	return true
}
